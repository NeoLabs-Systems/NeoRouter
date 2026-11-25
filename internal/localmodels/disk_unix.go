//go:build linux || darwin || freebsd || netbsd || openbsd

package localmodels

import "syscall"

// statfs measures the volume holding dir: free bytes available to this user and
// total bytes, the figures Node's fs.statfsSync returns.
func statfs(dir string) (free, total int64, ok bool) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(dir, &stats); err != nil {
		return 0, 0, false
	}
	blockSize := int64(stats.Bsize)
	return int64(stats.Bavail) * blockSize, int64(stats.Blocks) * blockSize, true
}
