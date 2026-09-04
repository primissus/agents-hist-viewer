package domain

// IndexDepth selects which conversation blocks are written to the search index.
type IndexDepth int

const (
	IndexDepthDeep IndexDepth = iota // default: all block kinds (images still dropped)
	IndexDepthQuick                  // user text, tool results, and plans only
)

// IndexOptions controls indexing transforms and depth.
type IndexOptions struct {
	Shrink ShrinkConfig
	Depth  IndexDepth
}

// DefaultIndexOptions is the recommended full index config.
var DefaultIndexOptions = IndexOptions{
	Shrink: DefaultShrinkConfig,
	Depth:  IndexDepthDeep,
}

// ShouldIndex reports whether a parsed block should be stored for search.
func (o IndexOptions) ShouldIndex(role Role, kind BlockKind) bool {
	switch o.Depth {
	case IndexDepthQuick:
		switch kind {
		case KindToolResult:
			return true
		case KindText:
			return role == RoleUser
		default:
			return false
		}
	default:
		return true
	}
}
