package pagetemplate

import "html/template"

var homeIcons = map[string]template.HTML{
	"wallet":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="6" width="18" height="13" rx="2"/><path d="M3 10h18M16 14h2"/></svg>`,
	"rate":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M7 17 17 7M8 9h.01M16 15h.01"/><circle cx="12" cy="12" r="9"/></svg>`,
	"settle":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 3v18M7 8l5-5 5 5M17 16l-5 5-5-5"/></svg>`,
	"shield":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 3 5 6v6c0 4 3 6.5 7 9 4-2.5 7-5 7-9V6z"/><path d="m9 12 2 2 4-4"/></svg>`,
	"code":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="m8 8-4 4 4 4M16 8l4 4-4 4M13 6l-2 12"/></svg>`,
	"chart":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 20V4M4 20h16M8 16l4-5 3 3 4-6"/></svg>`,
	"scan":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 8V5a1 1 0 0 1 1-1h3M20 8V5a1 1 0 0 0-1-1h-3M4 16v3a1 1 0 0 0 1 1h3M20 16v3a1 1 0 0 1-1 1h-3M3 12h18"/></svg>`,
	"media":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12a4 4 0 0 1 8 0M12 12v4"/></svg>`,
	"app":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="7" y="3" width="10" height="18" rx="2"/><path d="M11 18h2"/></svg>`,
	"h5":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 9v6M8 12h3M11 9v6M14 9h3M15.5 9v6"/></svg>`,
	"mini":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="M8 13c1.5 2 6.5 2 8 0"/></svg>`,
	"gateway": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="5" width="18" height="12" rx="2"/><path d="M3 20h18M9 17v3M15 17v3"/></svg>`,
	"check":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/></svg>`,
}

func homeIcon(key string) template.HTML { return homeIcons[key] }
