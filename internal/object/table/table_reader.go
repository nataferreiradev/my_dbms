package table

import objectcreator "my_DBMS/internal/object/object_creator"

type tableReader struct {
	objectCreator objectcreator.ObjectCreator
}

func NewTableReader(objectCreator objectcreator.ObjectCreator) *tableReader {
	return &tableReader{
		objectCreator: objectCreator,
	}
}

func (t *tableReader) Read(path string) ([]byte,error){
	return t.Read(path);
}
