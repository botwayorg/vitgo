package vitgo

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
)

type PackageJSON struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Type            string            `json:"type"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

type JSAppParams struct {
	JSHash        string `json:"hash"`
	ViteVersion   string `json:"vite_version"`
	ViteMajorVer  string `json:"vite_major_version"`
	PackageType   string `json:"package_type"`
	MajorVer      string `json:"major_version,omitempty"`
	EntryPoint    string `json:"entry_point"`
	HasTypeScript bool   `json:"has_ts"`
	IsVanilla     bool   `json:"is_vanilla,omitempty"`
	VueVersion    string `json:"vue_version,omitempty"`
	ReactVersion  string `json:"react_version,omitempty"`
	PreactVersion string `json:"preact_version,omitempty"`
	SvelteVersion string `json:"svelte_version,omitempty"`
	LitVersion    string `json:"lit_version,omitempty"`
}

func (vc *ViteConfig) parsePackageJSON() (*PackageJSON, error) {
	// If not set, try and find package.json
	path := ""

	if _, ok := vc.FS.(embed.FS); ok {
		path = vc.JSProjectPath + "/"
	}

	buf, err := fs.ReadFile(vc.FS, path+"package.json")

	if err != nil {
		return nil, err
	}

	content := PackageJSON{}
	err = json.Unmarshal(buf, &content)

	if err != nil {
		return nil, err
	}

	return &content, nil
}

func analyzePackageJSON(pkgJSON *PackageJSON) *JSAppParams {
	semVer := regexp.MustCompile(`^[\^]*((\d+)\.\d+\.\d+)$`)

	// parse for a ver; return the full version,
	// and the major version. Empty strings if
	// the version does not fit our regexp.
	getSemVer := func(verStr string) (string, string) {
		matches := semVer.FindStringSubmatch(verStr)

		var major string
		var fullVers string

		if matches != nil {
			major = matches[2]
			fullVers = matches[1]
		}

		return major, fullVers
	}

	output := JSAppParams{}

	// Is this actually a Vite package.json?
	if viteVers, ok := pkgJSON.DevDependencies["vite"]; ok {
		major, full := getSemVer(viteVers)
		output.ViteMajorVer = major
		output.ViteVersion = full
	} else {
		// Can't do anything with this package.json
		return nil
	}

	// TS?
	_, ok := pkgJSON.DevDependencies["typescript"]
	if ok {
		output.HasTypeScript = true
	}

	supported := []string{
		"vue",
		"react",
		"preact",
		"svelte", // devdep!
		"lit",    // won't really support
	}

	var vers string
	for _, pkg := range supported {
		if pkg == "svelte" {
			// special cased because svelte does not put
			// any configuration into dependencies.
			if sVer, ok := pkgJSON.DevDependencies["svelte"]; ok {
				vers = sVer
				major, full := getSemVer(vers)
				output.PackageType = pkg
				output.MajorVer = major
				output.SvelteVersion = full

				entryPt := "src/main.js"

				if output.HasTypeScript {
					entryPt = "src/main.ts"
				}

				output.EntryPoint = entryPt

				break
			}
		} else {
			if vers, ok = pkgJSON.Dependencies[pkg]; ok {
				output.PackageType = pkg
				major, full := getSemVer(vers)
				output.MajorVer = major

				// handle by category
				entryPt := "src/main.js" // most common case

				switch pkg {
				case "vue":
					output.VueVersion = full
					if output.HasTypeScript {
						entryPt = "src/main.ts"
					}

				case "react":
					output.ReactVersion = full
					if output.HasTypeScript {
						entryPt = "src/main.tsx"
					} else {
						entryPt = "src/main.jsx"
					}

				case "preact":
					output.PreactVersion = full
					if output.HasTypeScript {
						entryPt = "src/main.tsx"
					} else {
						entryPt = "src/main.jsx"
					}

				case "lit":
					output.LitVersion = full
					// we do not set entryPt;
					// lit is just too weird.
					entryPt = ""
				}

				// We know as much as we can...
				output.EntryPoint = entryPt
				break
			}
		}
	}

	// If we do not have type, call it Vanilla
	if output.PackageType == "" {
		output.IsVanilla = true
		output.PackageType = "vanilla"
		// Vite choses entry points anyway. For some
		// very odd reason, the JS project is flat,
		// and the TS project puts files in src/
		// Why? Good question.
		if output.HasTypeScript {
			output.EntryPoint = "src/main.ts"
		} else {
			output.EntryPoint = "main.js"
		}
	}

	return &output
}

func (vc *ViteConfig) getViteVersion() (string, error) {
	// If it's set, use it.
	if vc.ViteVersion != "" {
		return vc.ViteVersion, nil
	}

	if vc.DevDefaults == nil {
		return "", errors.New("not Vite project")
	}

	vc.ViteVersion = vc.DevDefaults.ViteMajorVer

	return vc.DevDefaults.ViteMajorVer, nil

}

func (vc *ViteConfig) SetDevelopmentDefaults() error {
	// Make sure we can find package.json:
	if vc.JSProjectPath == "" {
		vc.JSProjectPath = "frontend"
	}

	pkgJSON, err := vc.parsePackageJSON()
	if err != nil {
		return err
	}

	defaults := analyzePackageJSON(pkgJSON)
	if defaults == nil {
		return errors.New("invalid configuration")
	}

	vc.DevDefaults = defaults
	version, err := vc.getViteVersion()

	if err != nil {
		vc.ViteVersion = DEFAULT_VITE_VERSION
		version = vc.ViteVersion
	}

	// Check for anything already set, and if not set,
	// use the defaults if they are not set.
	if vc.Platform == "" {
		vc.Platform = defaults.PackageType
	}

	if vc.EntryPoint == "" {
		vc.EntryPoint = defaults.EntryPoint
	}

	if vc.URLPrefix == "" {
		// Vite default
		vc.URLPrefix = "/src/"
	}

	if vc.DevServerPort == "" {
		if version == "2" {
			vc.DevServerPort = DEFAULT_PORT_V2
		} else {
			vc.DevServerPort = DEFAULT_PORT_V3
		}
	}

	if vc.DevServerDomain == "" {
		vc.DevServerDomain = "localhost"
	}

	return nil

}

func (vc *ViteConfig) SetProductionDefaults() error {
	if vc.JSProjectPath == "" {
		vc.JSProjectPath = "frontend"
	}

	if vc.AssetsPath == "" {
		vc.AssetsPath = "dist"
	}

	if vc.URLPrefix == "" {
		vc.URLPrefix = "/assets/"
	}

	return nil
}

func (vc *ViteConfig) buildDevServerBaseURL() string {
	protocol := "http"
	if vc.HTTPS {
		protocol = "https"
	}

	return fmt.Sprintf(
		"%s://%s:%s",
		protocol,
		vc.DevServerDomain,
		vc.DevServerPort,
	)
}
