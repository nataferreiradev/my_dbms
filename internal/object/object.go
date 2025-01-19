package object

const TableExtension = ".marina"
const IndexExtension = ".marina"
const MetaDadosExtension = ".json"

type Object interface {
	Create(name string)
	Read(path string)
}
