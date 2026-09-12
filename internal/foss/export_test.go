package foss

// WriteDocumentForTest exposes the one function that creates a file, so that
// the structural guarantee of section 32.6 can be asserted against the code
// rather than only against the directory a run happened to produce.
func WriteDocumentForTest(directory, name, content string) error {
	return writeDocument(directory, name, content)
}
