package sshd

import (
	"sort"
	"strings"

	"github.com/nullpo7z/vantyx/internal/access"
)

// cliNavLocation is the user's position in the group tree and optional
// target.Path folder prefix within the current group.
type cliNavLocation struct {
	GroupPath  string // access group ID, "" at root
	PathPrefix string // folder within group from target.Path
}

func normalizeCLIPath(p string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimSpace(p), "//", "/"), "/")
}

func (loc cliNavLocation) pwd() string {
	if loc.GroupPath == "" && loc.PathPrefix == "" {
		return "/"
	}
	var parts []string
	if loc.GroupPath != "" {
		parts = append(parts, strings.Split(normalizeCLIPath(loc.GroupPath), "/")...)
	}
	if loc.PathPrefix != "" {
		parts = append(parts, strings.Split(normalizeCLIPath(loc.PathPrefix), "/")...)
	}
	return "/" + strings.Join(parts, "/")
}

func (loc cliNavLocation) parent() cliNavLocation {
	if loc.PathPrefix != "" {
		pp := normalizeCLIPath(loc.PathPrefix)
		if idx := strings.LastIndex(pp, "/"); idx >= 0 {
			return cliNavLocation{GroupPath: loc.GroupPath, PathPrefix: pp[:idx]}
		}
		return cliNavLocation{GroupPath: loc.GroupPath}
	}
	if loc.GroupPath == "" {
		return loc
	}
	gid := normalizeCLIPath(loc.GroupPath)
	if idx := strings.LastIndex(gid, "/"); idx >= 0 {
		return cliNavLocation{GroupPath: gid[:idx]}
	}
	return cliNavLocation{}
}

func (loc cliNavLocation) atRoot() bool {
	return loc.GroupPath == "" && loc.PathPrefix == ""
}

// cliNavItem is a numbered cd destination (subgroup or path folder).
type cliNavItem struct {
	Label      string
	Kind       string // "group" or "folder"
	GroupPath  string
	PathPrefix string
}

// cliNavView is what the menu shows at the current location.
type cliNavView struct {
	Location cliNavLocation
	Items    []cliNavItem
	Hosts    []*access.Target
}

func cliDirectChildGroups(all []cliGroupEntry, parentPath string) []cliGroupEntry {
	parentPath = normalizeCLIPath(parentPath)
	var out []cliGroupEntry
	prefix := ""
	if parentPath != "" {
		prefix = parentPath + "/"
	}
	for _, e := range all {
		gid := normalizeCLIPath(string(e.Group.ID))
		if parentPath == "" {
			if !strings.Contains(gid, "/") {
				out = append(out, e)
			}
			continue
		}
		if !strings.HasPrefix(gid, prefix) {
			continue
		}
		rest := strings.TrimPrefix(gid, prefix)
		if rest != "" && !strings.Contains(rest, "/") {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Group.Name < out[j].Group.Name
	})
	return out
}

func targetsInGroup(all []cliGroupEntry, groupPath string) []*access.Target {
	groupPath = normalizeCLIPath(groupPath)
	for _, e := range all {
		if normalizeCLIPath(string(e.Group.ID)) == groupPath {
			return e.Targets
		}
	}
	return nil
}

func buildCLINavView(all []cliGroupEntry, loc cliNavLocation) cliNavView {
	view := cliNavView{Location: loc}

	for _, c := range cliDirectChildGroups(all, loc.GroupPath) {
		view.Items = append(view.Items, cliNavItem{
			Label:     c.Group.Name,
			Kind:      "group",
			GroupPath: string(c.Group.ID),
		})
	}

	if loc.GroupPath != "" {
		folderSet := make(map[string]struct{})
		pp := normalizeCLIPath(loc.PathPrefix)
		for _, t := range targetsInGroup(all, loc.GroupPath) {
			tp := normalizeCLIPath(t.Path)
			remainder := tp
			if pp != "" {
				if tp == pp {
					continue
				}
				prefix := pp + "/"
				if !strings.HasPrefix(tp, prefix) {
					continue
				}
				remainder = strings.TrimPrefix(tp, prefix)
			}
			if remainder == "" {
				continue
			}
			if idx := strings.Index(remainder, "/"); idx >= 0 {
				folderSet[remainder[:idx]] = struct{}{}
			} else {
				folderSet[remainder] = struct{}{}
			}
		}
		folders := make([]string, 0, len(folderSet))
		for f := range folderSet {
			// Prefer real child groups over path folders with the same label.
			if pp == "" {
				skip := false
				for _, c := range cliDirectChildGroups(all, loc.GroupPath) {
					if c.Group.Name == f || normalizeCLIPath(string(c.Group.ID)) == normalizeCLIPath(loc.GroupPath)+"/"+f {
						skip = true
						break
					}
				}
				if skip {
					continue
				}
			}
			folders = append(folders, f)
		}
		sort.Strings(folders)
		for _, f := range folders {
			newPP := f
			if pp != "" {
				newPP = pp + "/" + f
			}
			view.Items = append(view.Items, cliNavItem{
				Label:      f + "/",
				Kind:       "folder",
				GroupPath:  loc.GroupPath,
				PathPrefix: newPP,
			})
		}

		for _, t := range targetsInGroup(all, loc.GroupPath) {
			tp := normalizeCLIPath(t.Path)
			if pp == "" || tp == pp {
				view.Hosts = append(view.Hosts, t)
			}
		}
		sort.Slice(view.Hosts, func(i, j int) bool {
			return view.Hosts[i].Name < view.Hosts[j].Name
		})
	}

	return view
}

func (loc cliNavLocation) cdIndex(all []cliGroupEntry, n int) (cliNavLocation, bool) {
	view := buildCLINavView(all, loc)
	if n < 1 || n > len(view.Items) {
		return loc, false
	}
	item := view.Items[n-1]
	switch item.Kind {
	case "group":
		return cliNavLocation{GroupPath: normalizeCLIPath(item.GroupPath)}, true
	case "folder":
		return cliNavLocation{GroupPath: item.GroupPath, PathPrefix: item.PathPrefix}, true
	default:
		return loc, false
	}
}

func targetIDsInNavScope(all []cliGroupEntry, loc cliNavLocation) map[string]bool {
	out := make(map[string]bool)
	locGroup := normalizeCLIPath(loc.GroupPath)
	locPath := normalizeCLIPath(loc.PathPrefix)

	if loc.atRoot() {
		for _, e := range all {
			for _, t := range e.Targets {
				out[string(t.ID)] = true
			}
		}
		return out
	}

	for _, e := range all {
		gid := normalizeCLIPath(string(e.Group.ID))
		inGroupTree := gid == locGroup || strings.HasPrefix(gid, locGroup+"/")
		for _, t := range e.Targets {
			tp := normalizeCLIPath(t.Path)
			if inGroupTree {
				if locPath == "" || tp == locPath || strings.HasPrefix(tp, locPath+"/") {
					out[string(t.ID)] = true
				}
				continue
			}
			if locPath == "" && strings.HasPrefix(tp, locGroup+"/") {
				out[string(t.ID)] = true
			}
		}
	}
	return out
}
