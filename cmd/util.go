package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/Masterminds/cookoo"
)

// Quiet, when set to true, can suppress Info and Debug messages.
var Quiet = false

// BeQuiet supresses Info and Debug messages.
func BeQuiet(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	Quiet = p.Get("quiet", false).(bool)
	return Quiet, nil
}

// ReadyToGlide fails if the environment is not sufficient for using glide.
//
// Most importantly, it fails if glide.yaml is not present in the current
// working directory.
func ReadyToGlide(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	fname := p.Get("filename", "glide.yaml").(string)
	if _, err := os.Stat(fname); err != nil {
		cwd, _ := os.Getwd()
		return false, fmt.Errorf("%s is missing from %s", fname, cwd)
	}
	return true, nil
}

// VersionGuard ensures that the Go version is correct.
func VersionGuard(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	cmd := exec.Command("go", "version")
	var out string
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, err
	} else if !strings.Contains(string(out), "go1.5") {
		Warn("You must install the Go 1.5 or greater toolchain to work with Glide.\n")
	}
	if os.Getenv("GO15VENDOREXPERIMENT") != "1" {
		Warn("To use Glide, you must set GO15VENDOREXPERIMENT=1\n")
	}

	// Verify the setup isn't for the old version of glide. That is, this is
	// no longer assuming the _vendor directory as the GOPATH. Inform of
	// the change.
	if _, err := os.Stat("_vendor/"); err == nil {
		Warn(`Your setup appears to be for the previous version of Glide.
Previously, vendor packages were stored in _vendor/src/ and
_vendor was set as your GOPATH. As of Go 1.5 the go tools
recognize the vendor directory as a location for these
files. Glide has embraced this. Please remove the _vendor
directory or move the _vendor/src/ directory to vendor/.` + "\n")
	}

	return out, nil
}

// CowardMode checks that the environment is setup before continuing on. If not
// setup and error is returned.
func CowardMode(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	gopath := os.Getenv("GOPATH")
	if len(gopath) == 0 {
		return false, fmt.Errorf("No GOPATH is set.\n")
	}

	_, err := os.Stat(path.Join(gopath, "src"))
	if err != nil {
		Error("Could not find %s/src.\n", gopath)
		Info("As of Glide 0.5/Go 1.5, this is required.\n")
		return false, err
	}

	return true, nil
}
