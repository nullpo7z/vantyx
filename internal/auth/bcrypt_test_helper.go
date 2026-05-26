package auth

import "golang.org/x/crypto/bcrypt"

// SetBcryptCostForTests overrides the bcrypt cost used by this
// package for the rest of the process. It exists so downstream test
// binaries (httpapi, sshd, ...) can keep their fixtures fast after
// the production default was bumped above bcrypt.DefaultCost (L-3).
//
// Production code must never call this; it is exposed only because
// the cost lives in an unexported variable and Go does not let test
// files share helpers across packages.
func SetBcryptCostForTests(cost int) {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return
	}
	bcryptCost = cost
}
