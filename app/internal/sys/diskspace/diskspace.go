package diskspace

import (
	"errors"
	"os"
	"path/filepath"
)

type Usage struct {
	Path   string `json:"path"`
	Volume string `json:"volume"`
	Free   uint64 `json:"free"`
	Total  uint64 `json:"total"`
}

func Of(path string) (Usage, error) {
	existing, err := nearestExisting(path)
	if err != nil {
		return Usage{}, err
	}
	u, err := statVolume(existing)
	if err != nil {
		return Usage{}, err
	}
	u.Path = existing
	return u, nil
}

func nearestExisting(path string) (string, error) {
	if path == "" {
		return "", errors.New("no folder given")
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", errors.New("no part of " + path + " exists")
		}
		p = parent
	}
}
