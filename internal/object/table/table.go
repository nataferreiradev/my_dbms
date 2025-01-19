package table

type TableObject struct {
	tableCreator TableCreator
}

func (t *TableObject) Create(name string) {
	t.tableCreator.CreateTable(name)
}

func (t *TableCreator) Read(path string) {

}