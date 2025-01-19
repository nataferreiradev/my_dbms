package v1

import (
	"errors"
	"os"
	"path/filepath"
)

type ObjectCreatorV1 struct {
	dataBasePath string
}

func NewObjectCreatorV1(dataBasePath string) *ObjectCreatorV1 {
	return &ObjectCreatorV1{
		dataBasePath: dataBasePath,
	}
}

func (o *ObjectCreatorV1) Create(fileName, extension string) (string, error) {
	if fileName == "" {
		return "", errors.New("File name can't be empty")
	}

	filePath := filepath.Join(o.dataBasePath, fileName+extension)

	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return filePath, nil
}
