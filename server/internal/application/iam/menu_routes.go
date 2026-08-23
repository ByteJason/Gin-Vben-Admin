package iam

import (
	"sort"
	"strings"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
)

// MenuRoute is the transport-neutral route record consumed by all three UI
// templates. It deliberately mirrors RouteRecordStringComponent instead of
// exposing database columns directly.
type MenuRoute struct {
	Name      string        `json:"name"`
	Path      string        `json:"path"`
	Component string        `json:"component"`
	Redirect  string        `json:"redirect,omitempty"`
	Meta      MenuRouteMeta `json:"meta"`
	Children  []MenuRoute   `json:"children,omitempty"`
}

type MenuRouteMeta struct {
	Title                    string   `json:"title"`
	Icon                     string   `json:"icon,omitempty"`
	Order                    int      `json:"order"`
	HideInMenu               bool     `json:"hideInMenu,omitempty"`
	KeepAlive                bool     `json:"keepAlive,omitempty"`
	Link                     string   `json:"link,omitempty"`
	OpenInNewWindow          bool     `json:"openInNewWindow,omitempty"`
	Authority                []string `json:"authority,omitempty"`
	MenuVisibleWithForbidden bool     `json:"menuVisibleWithForbidden,omitempty"`
}

// BuildMenuRoutes converts a flat, deterministically ordered menu collection
// into the route tree expected by the UI. Button permissions are intentionally
// not route nodes; their permission value remains available to management
// readers and writers. Inactive/hidden nodes and their descendants are not
// exposed to the dynamic route endpoint.
func BuildMenuRoutes(menus []domain.Menu) ([]MenuRoute, error) {
	normalized := make([]domain.Menu, 0, len(menus))
	byID := make(map[string]domain.Menu, len(menus))
	for _, menu := range menus {
		value, err := menu.NormalizeMenu()
		if err != nil {
			return nil, err
		}
		if _, exists := byID[value.ID]; exists {
			return nil, domain.ErrInvalidMenu
		}
		byID[value.ID] = value
		normalized = append(normalized, value)
	}
	// Detect parent cycles before constructing the tree. A cycle must fail the
	// whole projection rather than silently dropping a branch.
	for _, menu := range normalized {
		seen := map[string]struct{}{menu.ID: {}}
		parent := menu.ParentID
		for parent != "" {
			if _, exists := seen[parent]; exists {
				return nil, domain.ErrInvalidMenu
			}
			seen[parent] = struct{}{}
			next, exists := byID[parent]
			if !exists {
				break
			}
			parent = next.ParentID
		}
	}

	children := make(map[string][]domain.Menu, len(normalized))
	roots := make([]domain.Menu, 0, len(normalized))
	for _, menu := range normalized {
		if !menu.Active || !menu.Visible || menu.Type == domain.MenuTypeButton {
			continue
		}
		if menu.ParentID != "" {
			if parent, exists := byID[menu.ParentID]; exists && parent.Active && parent.Visible && parent.Type != domain.MenuTypeButton {
				children[menu.ParentID] = append(children[menu.ParentID], menu)
				continue
			}
		}
		roots = append(roots, menu)
	}
	orderMenus(roots)
	for key := range children {
		orderMenus(children[key])
	}

	var build func(domain.Menu, string) MenuRoute
	build = func(menu domain.Menu, parentPath string) MenuRoute {
		component := menu.Component
		if menu.Type == domain.MenuTypeDirectory && component == "" {
			component = "BasicLayout"
		}
		path := menu.Path
		if parentPath != "" {
			path = strings.TrimPrefix(path, parentPath)
			path = strings.TrimPrefix(path, "/")
			if path == "" {
				path = menu.Path
			}
		}
		route := MenuRoute{
			Name: menu.ID, Path: path, Component: component,
			Redirect: menu.Redirect,
			Meta: MenuRouteMeta{
				Title: menu.Name, Icon: menu.Icon, Order: menu.Sort,
				Authority: func() []string {
					if menu.Permission == "" {
						return nil
					}
					return []string{menu.Permission}
				}(),
				KeepAlive: menu.KeepAlive, Link: func() string {
					if menu.External {
						return menu.Redirect
					}
					return ""
				}(), OpenInNewWindow: menu.External,
			},
		}
		for _, child := range children[menu.ID] {
			route.Children = append(route.Children, build(child, menu.Path))
		}
		return route
	}
	result := make([]MenuRoute, 0, len(roots))
	for _, root := range roots {
		result = append(result, build(root, ""))
	}
	return result, nil
}

func orderMenus(values []domain.Menu) {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Sort != values[j].Sort {
			return values[i].Sort < values[j].Sort
		}
		return values[i].ID < values[j].ID
	})
}
