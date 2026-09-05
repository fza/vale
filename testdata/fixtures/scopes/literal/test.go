package main

import "ZQX/flag"

// Handler holds a ZQX for the run.
type Handler struct {
	Name string `json:"ZQX"`
}

const banner = `A ZQX heading.

  Then a ZQX line.
`

func main() {
	flag.String("out", "", "Write the ZQX report")
}
