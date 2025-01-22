package v1

import (
	"os"
)

type ObjectReaderV1 struct {

}

func Read(path string) ([]byte,error) {
	bytes,err := os.ReadFile(path)
	if err != nil {
		return bytes,err
	}
	return bytes,err
}