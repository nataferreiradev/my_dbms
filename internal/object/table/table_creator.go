package table

import (
	"my_DBMS/internal/object"
	objectcreator "my_DBMS/internal/object/object_creator"
)

type tableCreator struct {
	objectCreator objectcreator.ObjectCreator
}

func NewTableCreator(objectCreator objectcreator.ObjectCreator) *tableCreator {
	return &tableCreator{
		objectCreator: objectCreator,
	}
}

func (t *tableCreator) CreateTable(name string) (tablePath string, err error) {
	tablePath, err = t.objectCreator.Create(name, object.TableExtension)
	return
}
