package utils

import "golang.org/x/sys/unix"

func GetAvailableDiskStorageInMB(path string) (int, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	availableBytes := stat.Bavail * uint64(stat.Bsize)
	return int(availableBytes / 1024 / 1024), nil
}
