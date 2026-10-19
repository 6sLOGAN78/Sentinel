// Command detection-state reads private operator context; it never joins a group.
package main

import (
	"context"
	"io"
)

func main() {}

func runProtocol(context.Context, io.Reader, io.Writer, observer) error { return nil }
