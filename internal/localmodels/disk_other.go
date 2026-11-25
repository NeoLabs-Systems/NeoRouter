//go:build !(linux || darwin || freebsd || netbsd || openbsd)

package localmodels

// statfs is unavailable here, so disk space stays unknown — the same "no
// figure" the JS gets when fs.statfsSync throws.
func statfs(dir string) (free, total int64, ok bool) { return 0, 0, false }
