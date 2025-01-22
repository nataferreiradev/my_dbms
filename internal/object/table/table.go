package table

type Table struct {
	tableCreator tableCreator
	tableReader  tableReader
}

func (t *Table) Create(name string) {
	t.tableCreator.CreateTable(name)
}

func (t *Table) Read(path string) {
	t.tableReader.Read(path)
}
