package renter

import (
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/modules/renter/siadir"
	"gitlab.com/NebulousLabs/Sia/modules/renter/siafile"
	"gitlab.com/NebulousLabs/errors"
)

// bubbleStatus indicates the status of a bubble being executed on a
// directory
type bubbleStatus int

// bubbleError, bubbleInit, bubbleActive, and bubblePending are the constants
// used to determine the status of a bubble being executed on a directory
const (
	bubbleError bubbleStatus = iota
	bubbleInit
	bubbleActive
	bubblePending
)

// managedBubbleNeeded checks if a bubble is needed for a directory, updates the
// renter's bubbleUpdates map and returns a bool
func (r *Renter) managedBubbleNeeded(siaPath modules.SiaPath) (bool, error) {
	r.bubbleUpdatesMu.Lock()
	defer r.bubbleUpdatesMu.Unlock()

	// Check for bubble in bubbleUpdate map
	siaPathStr := siaPath.String()
	status, ok := r.bubbleUpdates[siaPathStr]
	if !ok {
		status = bubbleInit
		r.bubbleUpdates[siaPathStr] = status
	}

	// Update the bubble status
	var err error
	switch status {
	case bubblePending:
	case bubbleActive:
		r.bubbleUpdates[siaPathStr] = bubblePending
	case bubbleInit:
		r.bubbleUpdates[siaPathStr] = bubbleActive
		return true, nil
	default:
		err = errors.New("WARN: invalid bubble status")
	}
	return false, err
}

// managedCalculateDirectoryMetadata calculates the new values for the
// directory's metadata and tracks the value, either worst or best, for each to
// be bubbled up
func (r *Renter) managedCalculateDirectoryMetadata(siaPath modules.SiaPath) (siadir.Metadata, error) {
	// Set default metadata values to start
	metadata := siadir.Metadata{
		AggregateHealth:     siadir.DefaultDirHealth,
		AggregateNumFiles:   uint64(0),
		AggregateSize:       uint64(0),
		Health:              siadir.DefaultDirHealth,
		LastHealthCheckTime: time.Now(),
		MinRedundancy:       math.MaxFloat64,
		ModTime:             time.Time{},
		NumFiles:            uint64(0),
		NumStuckChunks:      uint64(0),
		NumSubDirs:          uint64(0),
		StuckHealth:         siadir.DefaultDirHealth,
	}
	// Read directory
	fileinfos, err := ioutil.ReadDir(siaPath.SiaDirSysPath(r.staticFilesDir))
	if err != nil {
		r.log.Printf("WARN: Error in reading files in directory %v : %v\n", siaPath.SiaDirSysPath(r.staticFilesDir), err)
		return siadir.Metadata{}, err
	}

	// Iterate over directory
	for _, fi := range fileinfos {
		// Check to make sure renter hasn't been shutdown
		select {
		case <-r.tg.StopChan():
			return siadir.Metadata{}, err
		default:
		}

		var aggregateHealth, stuckHealth, redundancy float64
		var numStuckChunks uint64
		var lastHealthCheckTime, modTime time.Time
		var fileMetadata siafile.BubbledMetadata
		ext := filepath.Ext(fi.Name())
		// Check for SiaFiles and Directories
		if ext == modules.SiaFileExtension {
			// SiaFile found, calculate the needed metadata information of the siafile
			fName := strings.TrimSuffix(fi.Name(), modules.SiaFileExtension)
			fileSiaPath, err := siaPath.Join(fName)
			if err != nil {
				r.log.Println("unable to join siapath with dirpath while calculating directory metadata:", err)
				continue
			}
			fileMetadata, err = r.managedCalculateAndUpdateFileMetadata(fileSiaPath)
			if err != nil {
				r.log.Printf("failed to calculate file metadata %v: %v", fi.Name(), err)
				continue
			}

			aggregateHealth = fileMetadata.Health
			stuckHealth = fileMetadata.StuckHealth
			redundancy = fileMetadata.Redundancy
			numStuckChunks = fileMetadata.NumStuckChunks
			lastHealthCheckTime = fileMetadata.LastHealthCheckTime
			modTime = fileMetadata.ModTime

			// Update aggregate fields.
			metadata.NumFiles++
			metadata.AggregateNumFiles++
			metadata.AggregateSize += fileMetadata.Size
		} else if fi.IsDir() {
			// Directory is found, read the directory metadata file
			dirSiaPath, err := siaPath.Join(fi.Name())
			if err != nil {
				return siadir.Metadata{}, err
			}
			dirMetadata, err := r.managedDirectoryMetadata(dirSiaPath)
			if err != nil {
				return siadir.Metadata{}, err
			}

			aggregateHealth = math.Max(dirMetadata.AggregateHealth, dirMetadata.Health)
			stuckHealth = dirMetadata.StuckHealth
			redundancy = dirMetadata.MinRedundancy
			numStuckChunks = dirMetadata.NumStuckChunks
			lastHealthCheckTime = dirMetadata.LastHealthCheckTime
			modTime = dirMetadata.ModTime

			// Update aggregate fields.
			metadata.AggregateNumFiles += dirMetadata.AggregateNumFiles
			metadata.AggregateSize += dirMetadata.AggregateSize
			metadata.NumSubDirs++
		} else {
			// Ignore everything that is not a SiaFile or a directory
			continue
		}
		// Update the Health of the directory based on the file Health
		if fileMetadata.Health > metadata.Health {
			metadata.Health = fileMetadata.Health
		}
		// Update the AggregateHealth
		if aggregateHealth > metadata.AggregateHealth {
			metadata.AggregateHealth = aggregateHealth
		}
		// Update Stuck Health
		if stuckHealth > metadata.StuckHealth {
			metadata.StuckHealth = stuckHealth
		}
		// Update MinRedundancy
		if redundancy < metadata.MinRedundancy {
			metadata.MinRedundancy = redundancy
		}
		// Update ModTime
		if modTime.After(metadata.ModTime) {
			metadata.ModTime = modTime
		}
		// Increment NumStuckChunks
		metadata.NumStuckChunks += numStuckChunks
		// Update LastHealthCheckTime if the file or sub directory
		// lastHealthCheckTime is older (before) the current lastHealthCheckTime
		if lastHealthCheckTime.Before(metadata.LastHealthCheckTime) {
			metadata.LastHealthCheckTime = lastHealthCheckTime
		}
		metadata.NumStuckChunks += numStuckChunks
	}
	// Sanity check on ModTime. If mod time is still zero it means there were no
	// files or subdirectories. Set ModTime to now since we just updated this
	// directory
	if metadata.ModTime.IsZero() {
		metadata.ModTime = time.Now()
	}

	// Sanity check on Redundancy. If MinRedundancy is still math.MaxFloat64
	// then set it to 0
	if metadata.MinRedundancy == math.MaxFloat64 {
		metadata.MinRedundancy = 0
	}

	return metadata, nil
}

// managedCalculateAndUpdateFileMetadata calculates and returns the necessary
// metadata information of a siafile that needs to be bubbled. The calculated
// metadata information is also updated and saved to disk
func (r *Renter) managedCalculateAndUpdateFileMetadata(siaPath modules.SiaPath) (siafile.BubbledMetadata, error) {
	// Load the Siafile.
	sf, err := r.staticFileSet.Open(siaPath)
	if err != nil {
		return siafile.BubbledMetadata{}, err
	}
	defer sf.Close()

	// Mark sure that healthy chunks are not marked as stuck
	hostOfflineMap, hostGoodForRenewMap, _ := r.managedRenterContractsAndUtilities([]*siafile.SiaFileSetEntry{sf})
	// TODO: This 'MarkAllHealthyChunksAsUnstuck' function may not be necessary
	// in the long term. I believe that it was/is useful because other parts of
	// the process for marking and handling stuck chunks was not complete.
	err = sf.MarkAllHealthyChunksAsUnstuck(hostOfflineMap, hostGoodForRenewMap)
	if err != nil {
		return siafile.BubbledMetadata{}, errors.AddContext(err, "unable to mark healthy chunks as unstuck")
	}
	// Calculate file health
	health, stuckHealth, numStuckChunks := sf.Health(hostOfflineMap, hostGoodForRenewMap)
	// Update the LastHealthCheckTime
	if err := sf.UpdateLastHealthCheckTime(); err != nil {
		return siafile.BubbledMetadata{}, err
	}
	// Calculate file Redundancy and check if local file is missing and
	// redundancy is less than one
	redundancy := sf.Redundancy(hostOfflineMap, hostGoodForRenewMap)
	if _, err := os.Stat(sf.LocalPath()); os.IsNotExist(err) && redundancy < 1 {
		r.log.Debugln("File not found on disk and possibly unrecoverable:", sf.LocalPath())
	}
	metadata := siafile.CachedHealthMetadata{
		Health:      health,
		Redundancy:  redundancy,
		StuckHealth: stuckHealth,
	}
	return siafile.BubbledMetadata{
		Health:              health,
		LastHealthCheckTime: sf.LastHealthCheckTime(),
		ModTime:             sf.ModTime(),
		NumStuckChunks:      numStuckChunks,
		Redundancy:          redundancy,
		Size:                sf.Size(),
		StuckHealth:         stuckHealth,
	}, sf.UpdateCachedHealthMetadata(metadata)
}

// managedCompleteBubbleUpdate completes the bubble update and updates and/or
// removes it from the renter's bubbleUpdates.
func (r *Renter) managedCompleteBubbleUpdate(siaPath modules.SiaPath) error {
	r.bubbleUpdatesMu.Lock()
	defer r.bubbleUpdatesMu.Unlock()

	// Check current status
	siaPathStr := siaPath.String()
	status, ok := r.bubbleUpdates[siaPathStr]
	if !ok {
		// Bubble not found in map, nothing to do.
		return nil
	}

	// Update status and call new bubble or remove from bubbleUpdates and save
	switch status {
	case bubblePending:
		r.bubbleUpdates[siaPathStr] = bubbleInit
		defer func() {
			go r.threadedBubbleMetadata(siaPath)
		}()
	case bubbleActive:
		delete(r.bubbleUpdates, siaPathStr)
	default:
		return errors.New("WARN: invalid bubble status")
	}

	return r.saveBubbleUpdates()
}

// managedDirectoryMetadata reads the directory metadata and returns the bubble
// metadata
func (r *Renter) managedDirectoryMetadata(siaPath modules.SiaPath) (siadir.Metadata, error) {
	// Check for bad paths and files
	fi, err := os.Stat(siaPath.SiaDirSysPath(r.staticFilesDir))
	if err != nil {
		return siadir.Metadata{}, err
	}
	if !fi.IsDir() {
		return siadir.Metadata{}, fmt.Errorf("%v is not a directory", siaPath)
	}

	//  Open SiaDir
	siaDir, err := r.staticDirSet.Open(siaPath)
	if os.IsNotExist(err) {
		// Remember initial Error
		initError := err
		// Metadata file does not exists, check if directory is empty
		fileInfos, err := ioutil.ReadDir(siaPath.SiaDirSysPath(r.staticFilesDir))
		if err != nil {
			return siadir.Metadata{}, err
		}
		// If the directory is empty and is not the root directory, assume it
		// was deleted so do not create a metadata file
		if len(fileInfos) == 0 && !siaPath.IsRoot() {
			return siadir.Metadata{}, initError
		}
		// If we are at the root directory or the directory is not empty, create
		// a metadata file
		siaDir, err = r.staticDirSet.NewSiaDir(siaPath)
	}
	if err != nil {
		return siadir.Metadata{}, err
	}
	defer siaDir.Close()

	return siaDir.Metadata(), nil
}

// threadedBubbleMetadata is the thread safe method used to call
// managedBubbleMetadata when the call does not need to be blocking
func (r *Renter) threadedBubbleMetadata(siaPath modules.SiaPath) {
	if err := r.tg.Add(); err != nil {
		return
	}
	defer r.tg.Done()
	if err := r.managedBubbleMetadata(siaPath); err != nil {
		r.log.Debugln("WARN: error with bubbling metadata:", err)
	}
}

// managedBubbleMetadata calculates the updated values of a directory's metadata
// and updates the siadir metadata on disk then calls threadedBubbleMetadata on
// the parent directory so that it is only blocking for the current directory
func (r *Renter) managedBubbleMetadata(siaPath modules.SiaPath) error {
	// Check if bubble is needed
	needed, err := r.managedBubbleNeeded(siaPath)
	if err != nil {
		return errors.AddContext(err, "error in checking if bubble is needed")
	}
	if !needed {
		return nil
	}

	// Make sure we call threadedBubbleMetadata on the parent once we are done.
	defer func() error {
		// Complete bubble
		err = r.managedCompleteBubbleUpdate(siaPath)
		if err != nil {
			return errors.AddContext(err, "error in completing bubble")
		}
		// Continue with parent dir if we aren't in the root dir already.
		if siaPath.IsRoot() {
			return nil
		}
		parentDir, err := siaPath.Dir()
		if err != nil {
			return errors.AddContext(err, "failed to defer threadedBubbleMetadata on parent dir")
		}
		go r.threadedBubbleMetadata(parentDir)
		return nil
	}()

	// Calculate the new metadata values of the directory
	metadata, err := r.managedCalculateDirectoryMetadata(siaPath)
	if err != nil {
		e := fmt.Sprintf("could not calculate the metadata of directory %v", siaPath.SiaDirSysPath(r.staticFilesDir))
		return errors.AddContext(err, e)
	}

	// Update directory metadata with the health information. Don't return here
	// to avoid skipping the repairNeeded and stuckChunkFound signals.
	siaDir, err := r.staticDirSet.Open(siaPath)
	if err != nil {
		e := fmt.Sprintf("could not open directory %v", siaPath.SiaDirSysPath(r.staticFilesDir))
		err = errors.AddContext(err, e)
	} else {
		defer siaDir.Close()
		err = siaDir.UpdateMetadata(metadata)
		if err != nil {
			e := fmt.Sprintf("could not update the metadata of the  directory %v", siaPath.SiaDirSysPath(r.staticFilesDir))
			err = errors.AddContext(err, e)
		}
	}

	// If we are at the root directory then check if any files were found in
	// need of repair or and stuck chunks and trigger the appropriate repair
	// loop. This is only done at the root directory as the repair and stuck
	// loops start at the root directory so there is no point triggering them
	// until the root directory is updated
	if siaPath.IsRoot() {
		if metadata.AggregateHealth >= siafile.RemoteRepairDownloadThreshold {
			select {
			case r.uploadHeap.repairNeeded <- struct{}{}:
			default:
			}
		}
		if metadata.NumStuckChunks > 0 {
			select {
			case r.uploadHeap.stuckChunkFound <- struct{}{}:
			default:
			}
		}
	}
	return err
}