// Package tools pins build-time tool dependencies so they are kept in
// go.sum. The package is gated by the "tools" build tag and contains
// no runtime code.
//
// Add new dev tools by importing them blankly here.
package tools
