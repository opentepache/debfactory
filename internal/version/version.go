// Package version identifica el commit de debfactory compilado. La
// instalación (just tool-install, specs/004-split-repos research D4) lo fija
// con -ldflags "-X debfactory/internal/version.Commit=<sha>"; sin eso vale
// "dev".
package version

// Commit es el commit completo de opentepache/debfactory, o "dev".
var Commit = "dev"
