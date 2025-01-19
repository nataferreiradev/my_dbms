package objectcreator

type ObjectCreator interface {
	Create(name, extension string) (path string, err error)
}
