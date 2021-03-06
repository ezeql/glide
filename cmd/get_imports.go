package cmd

import (
	"fmt"
	//"log"
	"os"
	"path"
	"regexp"
	"runtime"
	"strings"

	"github.com/Masterminds/cookoo"
	v "github.com/Masterminds/vcs"
)

func init() {
	// Precompile the regular expressions used to check VCS locations.
	for _, v := range vcsList {
		v.regex = regexp.MustCompile(v.pattern)
	}

	// Uncomment the line below and the log import to see the output
	// from the vcs commands executed for each project.
	//v.Logger = log.New(os.Stdout, "go-vcs", log.LstdFlags)
}

const (
	NoVCS = ""
	Git   = "git"
	Bzr   = "bzr"
	Hg    = "hg"
	Svn   = "svn"
)

// Get fetches a single package and puts it in vendor/.
//
// Params:
//	- package (string): Name of the package to get.
// 	- verbose (bool): default false
//
// Returns:
// 	- *Dependency: A dependency describing this package.
func Get(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	name := p.Get("package", "").(string)
	cfg := p.Get("conf", nil).(*Config)

	cwd, err := VendorPath(c)
	if err != nil {
		return nil, err
	}

	root := getRepoRootFromPackage(name)
	if len(root) == 0 {
		return nil, fmt.Errorf("Package name is required.")
	}

	if cfg.HasDependency(root) {
		return nil, fmt.Errorf("Package '%s' is already in glide.yaml", root)
	}

	dest := path.Join(cwd, root)
	repoUrl := "https://" + root
	repo, err := v.NewRepo(repoUrl, dest)
	if err != nil {
		return false, err
	}

	dep := &Dependency{
		Name:    root,
		VcsType: string(repo.Vcs()),

		// Should this assume a remote https root at all times?
		Repository: repoUrl,
	}
	subpkg := strings.TrimPrefix(name, root)
	if len(subpkg) > 0 && subpkg != "/" {
		dep.Subpackages = []string{subpkg}
	}

	if err := repo.Get(); err != nil {
		return dep, err
	}

	cfg.Imports = append(cfg.Imports, dep)

	return dep, nil
}

// GetImports iterates over the imported packages and gets them.
func GetImports(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	cfg := p.Get("conf", nil).(*Config)
	cwd, err := VendorPath(c)
	if err != nil {
		Error("Failed to prepare vendor directory: %s", err)
		return false, err
	}

	if len(cfg.Imports) == 0 {
		Info("No dependencies found. Nothing downloaded.\n")
		return false, nil
	}

	for _, dep := range cfg.Imports {
		if err := VcsGet(dep, cwd); err != nil {
			Warn("Skipped getting %s: %v\n", dep.Name, err)
		}
	}

	return true, nil
}

// UpdateImports iterates over the imported packages and updates them.
func UpdateImports(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	cfg := p.Get("conf", nil).(*Config)
	force := p.Get("force", true).(bool)
	cwd, err := VendorPath(c)
	if err != nil {
		return false, err
	}

	if len(cfg.Imports) == 0 {
		Info("No dependencies found. Nothing updated.\n")
		return false, nil
	}

	for _, dep := range cfg.Imports {
		if err := VcsUpdate(dep, cwd, force); err != nil {
			Warn("Update failed for %s: %s\n", dep.Name, err)
		}
	}

	return true, nil
}

func SetReference(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	cfg := p.Get("conf", nil).(*Config)
	cwd, err := VendorPath(c)
	if err != nil {
		return false, err
	}

	if len(cfg.Imports) == 0 {
		Info("No references set.\n")
		return false, nil
	}

	for _, dep := range cfg.Imports {
		if err := VcsVersion(dep, cwd); err != nil {
			Warn("Failed to set version on %s to %s: %s\n", dep.Name, dep.Reference, err)
		}
	}

	return true, nil
}

// filterArchOs indicates a dependency should be filtered out because it is
// the wrong GOOS or GOARCH.
func filterArchOs(dep *Dependency) bool {
	found := false
	if len(dep.Arch) > 0 {
		for _, a := range dep.Arch {
			if a == runtime.GOARCH {
				found = true
			}
		}
		// If it's not found, it should be filtered out.
		if !found {
			return true
		}
	}

	found = false
	if len(dep.Os) > 0 {
		for _, o := range dep.Os {
			if o == runtime.GOOS {
				found = true
			}
		}
		if !found {
			return true
		}

	}

	return false
}

func VcsExists(dep *Dependency, dest string) bool {
	repo, err := dep.GetRepo(dest)
	if err != nil {
		return false
	}

	return repo.CheckLocal()
}

// VcsGet figures out how to fetch a dependency, and then gets it.
//
// VcsGet installs into the dest.
func VcsGet(dep *Dependency, dest string) error {

	repo, err := dep.GetRepo(dest)
	if err != nil {
		return err
	}

	return repo.Get()
}

// VcsUpdate updates to a particular checkout based on the VCS setting.
func VcsUpdate(dep *Dependency, vend string, force bool) error {
	Info("Fetching updates for %s.\n", dep.Name)

	if filterArchOs(dep) {
		Info("%s is not used for %s/%s.\n", dep.Name, runtime.GOOS, runtime.GOARCH)
		return nil
	}

	dest := path.Join(vend, dep.Name)
	// If destination doesn't exist we need to perform an initial checkout.
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if err = VcsGet(dep, dest); err != nil {
			Warn("Unable to checkout %s\n", dep.Name)
			return err
		}
	} else {
		repo, err := dep.GetRepo(dest)

		// Tried to checkout a repo to a path that does not work. Either the
		// type or endpoint has changed. Force is being passed in so the old
		// location can be removed and replaced with the new one.
		// Warning, any changes in the old location will be deleted.
		// TODO: Put dirty checking in on the existing local checkout.
		if (err == v.ErrWrongVCS || err == v.ErrWrongRemote) && force == true {
			var newRemote string
			if len(dep.Repository) > 0 {
				newRemote = dep.Repository
			} else {
				newRemote = "https://" + dep.Name
			}

			Warn("Replacing %s with contents from %s\n", dep.Name, newRemote)
			rerr := os.RemoveAll(dest)
			if rerr != nil {
				return rerr
			}
			if err = VcsGet(dep, dest); err != nil {
				Warn("Unable to checkout %s\n", dep.Name)
				return err
			}
		} else if err != nil {
			return err
		} else {
			if err := repo.Update(); err != nil {
				Warn("Download failed.\n")
				return err
			}
		}
	}

	return nil
}

func VcsVersion(dep *Dependency, vend string) error {
	// If there is no refernece configured there is nothing to set.
	if dep.Reference == "" {
		return nil
	}

	Info("Setting version for %s.\n", dep.Name)

	cwd := path.Join(vend, dep.Name)
	repo, err := dep.GetRepo(cwd)
	if err != nil {
		return err
	}

	if err := repo.UpdateVersion(dep.Reference); err != nil {
		Error("Failed to set version to %s: %s\n", dep.Reference, err)
		return err
	}

	return nil
}

func VcsLastCommit(dep *Dependency, vend string) (string, error) {
	cwd := path.Join(vend, dep.Name)
	repo, err := dep.GetRepo(cwd)
	if err != nil {
		return "", err
	}

	version, err := repo.Version()
	if err != nil {
		return "", err
	}

	return version, nil
}

// From a package name find the root repo. For example,
// the package github.com/Masterminds/cookoo/io has a root repo
// at github.com/Masterminds/cookoo
func getRepoRootFromPackage(pkg string) string {
	for _, v := range vcsList {
		m := v.regex.FindStringSubmatch(pkg)
		if m == nil {
			continue
		}

		if m[1] != "" {
			return m[1]
		}
	}

	// Default to returning the package name passed in if no matches.
	// Should this be an error?
	return pkg
}

type vcsInfo struct {
	host    string
	pattern string
	regex   *regexp.Regexp
}

var vcsList = []*vcsInfo{
	{
		host:    "github.com",
		pattern: `^(?P<rootpkg>github\.com/[A-Za-z0-9_.\-]+/[A-Za-z0-9_.\-]+)(/[A-Za-z0-9_.\-]+)*$`,
	},
	{
		host:    "bitbucket.org",
		pattern: `^(?P<rootpkg>bitbucket\.org/([A-Za-z0-9_.\-]+/[A-Za-z0-9_.\-]+))(/[A-Za-z0-9_.\-]+)*$`,
	},
	{
		host:    "launchpad.net",
		pattern: `^(?P<rootpkg>launchpad\.net/(([A-Za-z0-9_.\-]+)(/[A-Za-z0-9_.\-]+)?|~[A-Za-z0-9_.\-]+/(\+junk|[A-Za-z0-9_.\-]+)/[A-Za-z0-9_.\-]+))(/[A-Za-z0-9_.\-]+)*$`,
	},
	{
		host:    "git.launchpad.net",
		pattern: `^(?P<rootpkg>git\.launchpad\.net/(([A-Za-z0-9_.\-]+)|~[A-Za-z0-9_.\-]+/(\+git|[A-Za-z0-9_.\-]+)/[A-Za-z0-9_.\-]+))$`,
	},
	{
		host:    "go.googlesource.com",
		pattern: `^(?P<rootpkg>go\.googlesource\.com/[A-Za-z0-9_.\-]+/?)$`,
	},
	// TODO: Once Google Code becomes fully deprecated this can be removed.
	{
		host:    "code.google.com",
		pattern: `^(?P<rootpkg>code\.google\.com/[pr]/([a-z0-9\-]+)(\.([a-z0-9\-]+))?)(/[A-Za-z0-9_.\-]+)*$`,
	},
	// Alternative Google setup. This is the previous structure but it still works... until Google Code goes away.
	{
		pattern: `^(?P<rootpkg>[a-z0-9_\-.]+)\.googlecode\.com/(git|hg|svn)(/.*)?$`,
	},
	// If none of the previous detect the type they will fall to this looking for the type in a generic sense
	// by the extension to the path.
	{
		pattern: `^(?P<rootpkg>(?P<repo>([a-z0-9.\-]+\.)+[a-z0-9.\-]+(:[0-9]+)?/[A-Za-z0-9_.\-/]*?)\.(bzr|git|hg|svn))(/[A-Za-z0-9_.\-]+)*$`,
	},
}
