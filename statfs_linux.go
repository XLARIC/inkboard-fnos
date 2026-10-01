//go:build linux

package main

import "syscall"

func volumeStats(path string) (Volume, error) {
	var s syscall.Statfs_t
	if e := syscall.Statfs(path, &s); e != nil {
		return Volume{}, e
	}
	total := s.Blocks * uint64(s.Bsize)
	used := (s.Blocks - s.Bfree) * uint64(s.Bsize)
	denom := used + s.Bavail*uint64(s.Bsize)
	v := Volume{Total: total, Used: used, Usage: unavailable("%", "容量统计不可用")}
	if denom > 0 {
		v.Usage = measured(float64(used)*100/float64(denom), "%")
	}
	return v, nil
}
