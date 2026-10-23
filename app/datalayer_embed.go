//go:build datalayer_embedded

package app

import _ "embed"

// embeddedDataLayer is the frozen data layer itself, so the app and the
// channel ship as one file. The release script writes app/datalayer.bin and
// builds with `-tags datalayer_embedded`.
//
//go:embed datalayer.bin
var embeddedDataLayer []byte
