package renter

// upload.go performs basic preprocessing on upload requests and then adds the
// requested files into the repair heap.
//
// TODO: Currently you cannot upload a directory using the api, if you want to
// upload a directory you must make 1 api call per file in that directory.
// Perhaps we should extend this endpoint to be able to recursively add files in
// a directory?
//
// TODO: Currently the minimum contracts check is not enforced while testing,
// which means that code is not covered at all. Enabling enforcement during
// testing will probably break a ton of existing tests, which means they will
// all need to be fixed when we do enable it, but we should enable it.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/crypto"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/modules/renter/siafile"
	"gitlab.com/NebulousLabs/writeaheadlog"
)

var (
	// errUploadDirectory is returned if the user tries to upload a directory.
	errUploadDirectory = errors.New("cannot upload directory")
)

// newFile is a helper to more easily create a new Siafile.
func newFile(siaFilePath string, name string, wal *writeaheadlog.WAL, rsc modules.ErasureCoder, sk crypto.CipherKey, fileSize uint64, mode os.FileMode, source string) (*siafile.SiaFile, error) {
	numChunks := 1
	chunkSize := (modules.SectorSize - sk.Type().Overhead()) * uint64(rsc.MinPieces())
	if fileSize > 0 {
		numChunks = int(fileSize / chunkSize)
		if fileSize%chunkSize != 0 {
			numChunks++
		}
	}
	ecs := make([]modules.ErasureCoder, numChunks)
	for i := 0; i < numChunks; i++ {
		ecs[i] = rsc
	}
	return siafile.New(siaFilePath, name, source, wal, ecs, sk, fileSize, mode)
}

// validateSource verifies that a sourcePath meets the
// requirements for upload.
func validateSource(sourcePath string) error {
	finfo, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if finfo.IsDir() {
		return errUploadDirectory
	}

	return nil
}

// Upload instructs the renter to start tracking a file. The renter will
// automatically upload and repair tracked files using a background loop.
func (r *Renter) Upload(up modules.FileUploadParams) error {
	// Enforce nickname rules.
	if err := validateSiapath(up.SiaPath); err != nil {
		return err
	}
	// Enforce source rules.
	if err := validateSource(up.Source); err != nil {
		return err
	}

	// Check for a nickname conflict.
	lockID := r.mu.RLock()
	_, exists := r.files[up.SiaPath]
	r.mu.RUnlock(lockID)
	if exists {
		return ErrPathOverload
	}

	// Fill in any missing upload params with sensible defaults.
	fileInfo, err := os.Stat(up.Source)
	if err != nil {
		return err
	}
	if up.ErasureCode == nil {
		up.ErasureCode, _ = siafile.NewRSCode(defaultDataPieces, defaultParityPieces)
	}

	// Check that we have contracts to upload to. We need at least data +
	// parity/2 contracts. NumPieces is equal to data+parity, and min pieces is
	// equal to parity. Therefore (NumPieces+MinPieces)/2 = (data+data+parity)/2
	// = data+parity/2.
	numContracts := len(r.hostContractor.Contracts())
	requiredContracts := (up.ErasureCode.NumPieces() + up.ErasureCode.MinPieces()) / 2
	if numContracts < requiredContracts && build.Release != "testing" {
		return fmt.Errorf("not enough contracts to upload file: got %v, needed %v", numContracts, (up.ErasureCode.NumPieces()+up.ErasureCode.MinPieces())/2)
	}

	// Create file object.
	siaFilePath := filepath.Join(r.persistDir, up.SiaPath+ShareExtension)
	// Create the path on disk.
	dir, _ := filepath.Split(siaFilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	cipherType := crypto.TypeDefaultRenter

	// Create the Siafile.
	f, err := newFile(siaFilePath, up.SiaPath, r.wal, up.ErasureCode, crypto.GenerateSiaKey(cipherType), uint64(fileInfo.Size()), fileInfo.Mode(), up.Source)
	if err != nil {
		return err
	}

	// Add file to renter.
	lockID = r.mu.Lock()
	r.files[up.SiaPath] = f
	r.persist.Tracking[up.SiaPath] = trackedFile{
		RepairPath: f.LocalPath(),
	}
	r.saveSync()
	r.mu.Unlock(lockID)

	// Send the upload to the repair loop.
	hosts := r.managedRefreshHostsAndWorkers()
	id := r.mu.Lock()
	unfinishedChunks := r.buildUnfinishedChunks(f, hosts)
	r.mu.Unlock(id)
	for i := 0; i < len(unfinishedChunks); i++ {
		r.uploadHeap.managedPush(unfinishedChunks[i])
	}
	select {
	case r.uploadHeap.newUploads <- struct{}{}:
	default:
	}
	return nil
}
