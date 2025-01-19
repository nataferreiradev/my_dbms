package table

import (
	"my_DBMS/internal/object"
	objectcreator "my_DBMS/internal/object/object_creator"
)

type TableCreator struct {
	objectCreator objectcreator.ObjectCreator
}

func NewTableCreator(objectcreator objectcreator.ObjectCreator) *TableCreator {
	return &TableCreator{
		objectCreator: objectcreator,
	}
}

func (t *TableCreator) CreateTable(name string) (tablePath string, err error) {
	tablePath, err = t.objectCreator.Create(name, object.TableExtension)
	return
}
