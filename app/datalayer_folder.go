//go:build !datalayer_embedded

package app

// embeddedDataLayer is empty in a build that ships the data layer beside the
// executable, or runs it from the project.
var embeddedDataLayer []byte
