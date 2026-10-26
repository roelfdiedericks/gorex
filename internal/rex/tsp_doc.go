package rex

import (
	"encoding/json"
	"fmt"
)

// tspDoc indexes surfaces' documents and applies frame ops to them: the
// server keeps the documents its programs draw, and each window mirrors
// them (SurfaceMirror), applying the same ops to the same nodes.
type tspDoc struct {
	// nodes index each surface's nodes by id, and parent each node's
	// parent, "" for the root; both by the surface's key (surfaceKey).
	nodes  map[string]map[string]*TSPNode
	parent map[string]map[string]string
}

// surfaceKey names a surface in a document: its id and its anchor, since a
// program that runs again opens its surfaces under the same ids while
// those it kept stay.
func surfaceKey(sf *Surface) string { return fmt.Sprintf("%s#%d", sf.ID, sf.Image) }

// index indexes a surface's nodes from its root, replacing what the
// document had of it.
func (d *tspDoc) index(sf *Surface) {
	if d.nodes == nil {
		d.nodes, d.parent = map[string]map[string]*TSPNode{}, map[string]map[string]string{}
	}
	nodes, parent := map[string]*TSPNode{}, map[string]string{}
	var walk func(n *TSPNode, p string)
	walk = func(n *TSPNode, p string) {
		nodes[n.ID], parent[n.ID] = n, p
		for _, c := range n.Children {
			walk(c, n.ID)
		}
	}
	if sf.Root != nil {
		walk(sf.Root, "")
	}
	key := surfaceKey(sf)
	d.nodes[key], d.parent[key] = nodes, parent
}

// drop forgets a surface's nodes.
func (d *tspDoc) drop(sf *Surface) {
	key := surfaceKey(sf)
	delete(d.nodes, key)
	delete(d.parent, key)
}

// apply applies a frame op to a surface's document. It returns the op's
// name, with its node and argument for the ops that change nothing kept
// (settle, focus, reveal, scroll), which a window acts on.
func (d *tspDoc) apply(sf *Surface, raw json.RawMessage) (name, id, arg string) {
	var op []json.RawMessage
	if json.Unmarshal(raw, &op) != nil || len(op) == 0 {
		return "", "", ""
	}
	if json.Unmarshal(op[0], &name) != nil {
		return "", "", ""
	}
	str := func(i int) string {
		var s string
		if i < len(op) {
			json.Unmarshal(op[i], &s)
		}
		return s
	}
	key := surfaceKey(sf)
	nodes, parents := d.nodes[key], d.parent[key]
	if nodes == nil {
		return name, str(1), str(2)
	}
	switch name {
	case "add":
		// ["add", id, parent, before, node]
		if len(op) < 5 {
			return
		}
		var n TSPNode
		if json.Unmarshal(op[4], &n) != nil || n.ID == "" {
			return
		}
		d.remove(sf, n.ID)
		parent, before := str(2), str(3)
		d.insert(sf, &n, parent, before)
		var index func(n *TSPNode, parent string)
		index = func(n *TSPNode, parent string) {
			nodes[n.ID], parents[n.ID] = n, parent
			for _, c := range n.Children {
				index(c, n.ID)
			}
		}
		index(&n, parent)
		patchItems(n.Props)
	case "set":
		// ["set", id, props]: a null prop deletes it.
		n := nodes[str(1)]
		if n == nil || len(op) < 3 {
			return
		}
		var props map[string]any
		if json.Unmarshal(op[2], &props) != nil {
			return
		}
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		for k, v := range props {
			if v == nil {
				delete(n.Props, k)
			} else {
				n.Props[k] = v
			}
		}
		patchItems(n.Props)
	case "text":
		// ["text", id, "append"|"replace", text]
		n := nodes[str(1)]
		if n == nil || len(op) < 4 {
			return
		}
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		text, _ := n.Props["text"].(string)
		if str(2) == "append" {
			n.Props["text"] = text + str(3)
		} else {
			n.Props["text"] = str(3)
		}
	case "splice":
		// ["splice", id, at, del, text]: at and del count UTF-16 units.
		n := nodes[str(1)]
		if n == nil || len(op) < 5 {
			return
		}
		var at, del int
		json.Unmarshal(op[2], &at)
		json.Unmarshal(op[3], &del)
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		text, _ := n.Props["text"].(string)
		n.Props["text"] = spliceUTF16(text, at, del, str(4))
	case "move":
		// ["move", id, parent, before]
		n := nodes[str(1)]
		if n == nil {
			return
		}
		d.remove(sf, n.ID)
		d.insert(sf, n, str(2), str(3))
		parents[n.ID] = str(2)
		nodes[n.ID] = n
	case "del":
		d.remove(sf, str(1))
		var drop func(id string)
		drop = func(id string) {
			if n := nodes[id]; n != nil {
				for _, c := range n.Children {
					drop(c.ID)
				}
			}
			delete(nodes, id)
			delete(parents, id)
		}
		drop(str(1))
	default:
		// settle, focus, reveal, scroll, suspend, resume: hints, nothing
		// kept.
		return name, str(1), str(2)
	}
	return name, "", ""
}

// insert puts n under parent (the surface itself when parent is the
// surface's id or unknown) before the child before, at the end when "".
func (d *tspDoc) insert(sf *Surface, n *TSPNode, parent, before string) {
	nodes := d.nodes[surfaceKey(sf)]
	if parent == "" || nodes[parent] == nil {
		parent = sf.ID
	}
	p := nodes[parent]
	if p == nil {
		return
	}
	at := len(p.Children)
	if before != "" {
		for i, c := range p.Children {
			if c.ID == before {
				at = i
				break
			}
		}
	}
	p.Children = append(p.Children, nil)
	copy(p.Children[at+1:], p.Children[at:])
	p.Children[at] = n
}

// remove takes the node id out of its parent's children, keeping it in
// the index; the root stays.
func (d *tspDoc) remove(sf *Surface, id string) {
	key := surfaceKey(sf)
	n := d.nodes[key][id]
	if n == nil || n == sf.Root {
		return
	}
	p := d.nodes[key][d.parent[key][id]]
	if p == nil {
		return
	}
	for i, c := range p.Children {
		if c == n {
			p.Children = append(p.Children[:i], p.Children[i+1:]...)
			return
		}
	}
}

// SurfaceUpdate is what changed in a session's surfaces since the update
// before it (V-1), which the server sends each window that has that one:
// the surfaces opened, with their documents as they were opened, the
// frames applied to them, and the surfaces there are now, in their
// order, without their documents.
type SurfaceUpdate struct {
	V      uint64            `json:"v"`
	Opens  []json.RawMessage `json:"opens,omitempty"`
	Frames []SurfaceFrame    `json:"frames,omitempty"`
	List   []Surface         `json:"list"`
}

// SurfaceFrame is the ops a program applied to one of its surfaces.
type SurfaceFrame struct {
	SF    string            `json:"sf"`
	Image uint32            `json:"image,omitempty"`
	Ops   []json.RawMessage `json:"ops"`
}

// SurfaceHint is an op that keeps nothing, for a window to act on: a
// node to scroll into view (reveal), a container to scroll (scroll), a
// field to focus (focus), a node done animating (settle).
type SurfaceHint struct {
	SF    string
	Image uint32
	Op    string
	ID    string
	Arg   string
}

// SurfaceMirror is a window's copy of a session's surfaces, made from
// the whole set the server sends on attach (Reset) and kept by the
// updates after it (Update). Its methods are not safe for concurrent
// use: a window calls them on its UI thread.
type SurfaceMirror struct {
	doc      tspDoc
	surfaces map[string]*Surface
	list     []Surface
	v        uint64
}

// Reset takes the session's surfaces whole.
func (m *SurfaceMirror) Reset(set *SurfaceSet) {
	m.doc = tspDoc{}
	m.surfaces = map[string]*Surface{}
	list := make([]Surface, 0, len(set.Surfaces))
	for _, sf := range set.Surfaces {
		m.surfaces[surfaceKey(sf)] = sf
		m.doc.index(sf)
		list = append(list, *sf)
	}
	m.v = set.V
	m.relist(list)
}

// Update applies an update. It reports false when the update does not
// follow the version the mirror has, which then needs the whole set
// again; and the hints of its frames.
func (m *SurfaceMirror) Update(u SurfaceUpdate) (hints []SurfaceHint, ok bool) {
	if m.surfaces == nil || u.V != m.v+1 {
		return nil, false
	}
	for _, raw := range u.Opens {
		var sf Surface
		if json.Unmarshal(raw, &sf) != nil {
			continue
		}
		m.surfaces[surfaceKey(&sf)] = &sf
		m.doc.index(&sf)
	}
	for _, f := range u.Frames {
		sf := m.surfaces[surfaceKey(&Surface{ID: f.SF, Image: f.Image})]
		if sf == nil {
			continue
		}
		for _, op := range f.Ops {
			if name, id, arg := m.doc.apply(sf, op); id != "" {
				hints = append(hints, SurfaceHint{SF: f.SF, Image: f.Image, Op: name, ID: id, Arg: arg})
			}
		}
	}
	m.v = u.V
	m.relist(u.List)
	return hints, true
}

// relist makes the list the surfaces listed, in order, each with what
// the list says of it and the document the mirror has; it forgets the
// others.
func (m *SurfaceMirror) relist(list []Surface) {
	keep := map[string]bool{}
	m.list = make([]Surface, 0, len(list))
	for _, meta := range list {
		key := surfaceKey(&meta)
		sf := m.surfaces[key]
		if sf == nil {
			continue
		}
		root := sf.Root
		*sf = meta
		sf.Root = root
		keep[key] = true
		m.list = append(m.list, *sf)
	}
	for key, sf := range m.surfaces {
		if !keep[key] {
			m.doc.drop(sf)
			delete(m.surfaces, key)
		}
	}
}

// List returns the surfaces, in the server's order.
func (m *SurfaceMirror) List() []Surface { return m.list }

// Version is the version of the surfaces the mirror has.
func (m *SurfaceMirror) Version() uint64 { return m.v }
