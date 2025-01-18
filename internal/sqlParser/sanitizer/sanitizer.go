package sanitizer

type Sanitizer interface {
	Sanitize(input string) string
}
