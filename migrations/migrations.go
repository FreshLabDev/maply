// SPDX-License-Identifier: Apache-2.0

// Package migrations embeds the SQL migrations into the binary, so the image
// carries exactly the schema its code was built against.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
