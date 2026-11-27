// Package sourceprofile builds typed profiles describing a project's source tree.
// It exposes OPS-owned models while delegating file discovery to SRCpack.
package sourceprofile

import (
	"fmt"

	srcpackconfig "github.com/nexssp/srcpack/bundles/config"
)

// SourceTreeProfileOptions describes a source tree to turn into a profile.
// Types may contain comma-separated aliases or extensions; unknown aliases are
// treated as extensions. TypeDefinitions maps aliases to extensions.
// Extensions adds comma-separated raw extensions, and ExcludeDirs adds
// root-relative directory names or paths to the built-in exclusions.
type SourceTreeProfileOptions struct {
	Root            string
	ProfileName     string
	Types           []string
	TypeDefinitions map[string][]string
	Extensions      string
	ExcludeDirs     []string
}

// SourceTreeProfile is the typed result of selecting source directories.
// Directories are sorted, root-relative, and collapsed so selected parents
// replace their descendants. An empty ExtensionFilter means all file types.
type SourceTreeProfile struct {
	Name        string
	BaseDir     string
	Description string
	Options     ProfileOptions
	Directories []string
}

// ProfileOptions contains the profile settings relevant to source-tree builds.
type ProfileOptions struct {
	RemoveBodyFunc  bool
	ExportedOnly    bool
	ExtensionFilter string
}

// BuildSourceTreeProfile discovers matching files under Root and returns an
// OPS-owned typed profile. An empty Root selects the current working directory.
// The result does not encode YAML; callers can compose it with application
// configuration without importing SRCpack's config model.
func BuildSourceTreeProfile(options SourceTreeProfileOptions) (SourceTreeProfile, error) {
	generated, err := srcpackconfig.BuildSourceTreeProfile(srcpackconfig.SourceTreeProfileOptions{
		Root:            options.Root,
		ProfileName:     options.ProfileName,
		Types:           options.Types,
		TypeDefinitions: options.TypeDefinitions,
		Extensions:      options.Extensions,
		ExcludeDirs:     options.ExcludeDirs,
	})
	if err != nil {
		return SourceTreeProfile{}, fmt.Errorf("build source tree profile: %w", err)
	}
	if len(generated.Profiles) != 1 {
		return SourceTreeProfile{}, fmt.Errorf("build source tree profile: SRCpack returned %d profiles, want 1", len(generated.Profiles))
	}

	var profile srcpackconfig.Profile
	for _, profile = range generated.Profiles {
		break
	}

	result := SourceTreeProfile{
		Name:        profile.Name,
		BaseDir:     profile.BaseDir,
		Description: profile.Description,
		Options: ProfileOptions{
			RemoveBodyFunc: profile.Options.RemoveBodyFunc != nil && *profile.Options.RemoveBodyFunc,
			ExportedOnly:   profile.Options.ExportedOnly != nil && *profile.Options.ExportedOnly,
		},
		Directories: make([]string, 0, len(profile.Dir)),
	}
	if profile.Options.ExtFilter != nil {
		result.Options.ExtensionFilter = *profile.Options.ExtFilter
	}
	for _, dir := range profile.Dir {
		result.Directories = append(result.Directories, dir.Path)
	}

	return result, nil
}
