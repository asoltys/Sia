module gitlab.com/NebulousLabs/Sia

go 1.12

require (
	github.com/dchest/threefish v0.0.0-20120919164726-3ecf4c494abf
	github.com/hanwen/go-fuse v1.0.1-0.20190720155834-4dd6878445ae
	github.com/hanwen/go-fuse/v2 v2.0.2
	github.com/inconshreveable/go-update v0.0.0-20160112193335-8152e7eb6ccf
	github.com/julienschmidt/httprouter v1.2.0
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0
	github.com/karrick/godirwalk v1.10.12
	github.com/klauspost/cpuid v1.2.1 // indirect
	github.com/klauspost/reedsolomon v1.9.2
	github.com/pkg/errors v0.8.1 // indirect
	github.com/spf13/cobra v0.0.4
	github.com/xtaci/smux v1.3.3
	gitlab.com/NebulousLabs/bolt v1.4.0
	gitlab.com/NebulousLabs/demotemutex v0.0.0-20151003192217-235395f71c40
	gitlab.com/NebulousLabs/entropy-mnemonics v0.0.0-20181018051301-7532f67e3500
	gitlab.com/NebulousLabs/errors v0.0.0-20171229012116-7ead97ef90b8
	gitlab.com/NebulousLabs/fastrand v0.0.0-20181126182046-603482d69e40
	gitlab.com/NebulousLabs/go-upnp v0.0.0-20181011194642-3a71999ed0d3
	gitlab.com/NebulousLabs/merkletree v0.0.0-20190207030457-bc4a11e31a0d
	gitlab.com/NebulousLabs/monitor v0.0.0-20191205095550-2b0fd3e1012a
	gitlab.com/NebulousLabs/ratelimit v0.0.0-20180716154200-1308156c2eaf
	gitlab.com/NebulousLabs/threadgroup v0.0.0-20180716154133-88a11db9e46c
	gitlab.com/NebulousLabs/writeaheadlog v0.0.0-20190729190618-012a9d4274dd
	golang.org/x/crypto v0.0.0-20191105034135-c7e5f84aec59
	golang.org/x/lint v0.0.0-20191125180803-fdd1cda4f05f // indirect
	golang.org/x/tools v0.0.0-20191125144606-a911d9008d1f
	honnef.co/go/tools v0.0.1-2019.2.3 // indirect
)

replace github.com/xtaci/smux => ./vendor/github.com/xtaci/smux
