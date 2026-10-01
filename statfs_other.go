//go:build !linux

package main

import "errors"

func volumeStats(path string) (Volume, error) {
	return Volume{}, errors.New("主机采集仅支持 Linux")
}
