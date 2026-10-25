package editor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gongahkia/shack/internal/fileops"
	"github.com/gongahkia/shack/internal/lsp"
	"github.com/gongahkia/shack/internal/text"
)

type resourceSnapshot struct {
	Buffer         *Buffer
	File           File
	Doc            *text.Document
	Title, Newline string
	Transient      bool
}
type resourceBuffer struct {
	Before, After    resourceSnapshot
	New              bool
	Forward, Reverse [][]text.Edit
}
type resourcePlan struct {
	HasFiles      bool
	Root          string
	Before, After map[string]fileops.Node
	Buffers       []*resourceBuffer
	Rows          []*workspaceTarget
	Labels        []string
}
type resourceChange struct {
	Kind, URI, OldURI, NewURI string
	AnnotationID              string `json:"annotationId"`
	Options                   struct{ Overwrite, IgnoreIfExists, Recursive, IgnoreIfNotExists bool }
	TextDocument              struct {
		URI     string
		Version *uint64
	}
	Edits []lsp.TextEdit
}

func hasResourceChanges(raw json.RawMessage) bool {
	var w wireWorkspaceEdit
	if json.Unmarshal(raw, &w) != nil {
		return false
	}
	if len(w.ChangeAnnotations) > 0 {
		return true
	}
	for _, edits := range w.Changes {
		for _, edit := range edits {
			if edit.AnnotationID != "" {
				return true
			}
		}
	}
	seen := map[string]bool{}
	for _, raw := range w.DocumentChanges {
		var c resourceChange
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		for _, edit := range c.Edits {
			if edit.AnnotationID != "" {
				return true
			}
		}
		if c.Kind != "" || seen[c.TextDocument.URI] {
			return true
		}
		seen[c.TextDocument.URI] = true
	}
	return false
}

// The worker receives immutable document clones and file metadata, never reads
// live Buffer fields, and leaves both disk and the editor untouched.
func prepareResources(ctx context.Context, root string, raw json.RawMessage, sources []resourceSnapshot) (*resourcePlan, error) {
	if len(raw) > workspaceBytes {
		return nil, errors.New("workspace edit exceeds 8 MiB")
	}
	plan, err := fileops.New(root)
	if err != nil {
		return nil, err
	}
	var wire wireWorkspaceEdit
	if err = json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	if wire.DocumentChanges == nil {
		uris := []string{}
		for uri := range wire.Changes {
			uris = append(uris, uri)
		}
		sort.Strings(uris)
		for _, uri := range uris {
			c := resourceChange{Edits: wire.Changes[uri]}
			c.TextDocument.URI = uri
			data, _ := json.Marshal(c)
			wire.DocumentChanges = append(wire.DocumentChanges, data)
		}
	}
	if len(wire.DocumentChanges) > workspaceMatches {
		return nil, errors.New("workspace edit exceeds 2,000 operations")
	}
	result := &resourcePlan{Root: plan.Root}
	for _, raw := range wire.DocumentChanges {
		var c resourceChange
		if json.Unmarshal(raw, &c) == nil && c.Kind != "" {
			result.HasFiles = true
		}
	}
	work := map[string]*resourceBuffer{}
	origins := map[string]string{}
	for _, s := range sources {
		if !fileops.Inside(plan.Root, s.File.Path) {
			continue
		}
		work[s.File.Path] = &resourceBuffer{Before: s, After: s}
	}
	touched := map[*resourceBuffer]bool{}
	bytes, count, rowBytes := 0, 0, 0
	touch := func(b *resourceBuffer) error {
		if touched[b] {
			return nil
		}
		bytes += b.Before.Doc.Text.Len() + b.After.Doc.Text.Len()
		if bytes > 2*workspaceBytes {
			return errors.New("workspace edit exceeds 8 MiB of source/expanded text")
		}
		touched[b] = true
		result.Buffers = append(result.Buffers, b)
		b.After.Doc = b.After.Doc.Clone()
		if b.Before.File.Path != "" {
			_, e := plan.Read(b.Before.File.Path)
			if e != nil {
				return e
			}
			original := plan.Before[b.Before.File.Path]
			if (original.Kind == fileops.File) != b.Before.File.exists || (original.Kind == fileops.File && sha256.Sum256([]byte(original.Data)) != b.Before.File.hash) {
				return errors.New("open resource changed on disk")
			}
		}
		return nil
	}
	load := func(path string) (*resourceBuffer, error) {
		if b := work[path]; b != nil {
			if err := touch(b); err != nil {
				return nil, err
			}
			return b, nil
		}
		n, err := plan.Read(path)
		if err != nil {
			return nil, err
		}
		if n.Kind != fileops.File {
			return nil, fmt.Errorf("text edit targets a missing/non-file resource: %s", path)
		}
		if strings.IndexByte(n.Data, 0) >= 0 {
			return nil, errors.New("text edit targets a binary file")
		}
		d, err := text.NewDocument(n.Data)
		if err != nil {
			return nil, err
		}
		b := &Buffer{}
		s := resourceSnapshot{Buffer: b, File: File{Path: path, exists: true, hash: sha256.Sum256([]byte(n.Data))}, Doc: d, Newline: detectNewline(d.Text)}
		original := s
		if origin, ok := origins[path]; ok && origin != "" && origin != path {
			n := plan.Before[origin]
			if n.Kind == fileops.File {
				d, err := text.NewDocument(n.Data)
				if err != nil {
					return nil, err
				}
				original.Doc = d
				original.File = File{Path: origin, exists: true, hash: sha256.Sum256([]byte(n.Data))}
			}
		}
		item := &resourceBuffer{Before: original, After: s, New: true}
		work[path] = item
		// A closed file may already have moved. Its original disk guard is in the
		// filesystem plan; the synthetic buffer does not refer to the old URI.
		touched[item] = true
		result.Buffers = append(result.Buffers, item)
		bytes += 2 * len(n.Data)
		if bytes > 2*workspaceBytes {
			return nil, errors.New("workspace text snapshot exceeds 8 MiB")
		}
		item.After.Doc = d.Clone()
		return item, nil
	}
	row := func(label, before, after string) error {
		count++
		rowBytes += len(before) + len(after)
		if rowBytes > workspaceBytes {
			return errors.New("workspace preview exceeds 8 MiB")
		}
		if count > workspaceMatches {
			return errors.New("workspace edit exceeds 2,000 changes")
		}
		before = strings.ToValidUTF8(before, "�")
		after = strings.ToValidUTF8(after, "�")
		d, _ := text.NewDocument(before)
		result.Labels = append(result.Labels, label)
		result.Rows = append(result.Rows, &workspaceTarget{Buffer: &Buffer{File: &File{}, Doc: d, Title: label}, Document: d, Edits: []text.Edit{{Start: 0, End: len(before), Text: after}}})
		return nil
	}
	contents := func(label string, image map[string]fileops.Node) error {
		paths := make([]string, 0, len(image))
		for path, n := range image {
			if n.Kind == fileops.File {
				paths = append(paths, path)
			}
		}
		sort.Strings(paths)
		for _, path := range paths {
			rel, _ := filepath.Rel(plan.Root, path)
			if err := row(label+" "+rel, resourceSummary(image[path]), "Removed from this path"); err != nil {
				return err
			}
		}
		return nil
	}
	annotations := map[string]bool{}
	annotate := func(id string) error {
		if id == "" || annotations[id] {
			return nil
		}
		annotation, ok := wire.ChangeAnnotations[id]
		if !ok || annotation.Label == "" {
			return errors.New("workspace edit refers to an unknown change annotation")
		}
		annotations[id] = true
		note := "Included in this action"
		if annotation.NeedsConfirmation {
			note = "Accepting this whole action confirms this annotated change"
		}
		return row(fmt.Sprintf("Annotation: %.160s", annotation.Label), annotation.Description, note)
	}
	apply := func(item *resourceBuffer, edits []text.Edit) error {
		beforeVersion := item.After.Doc.Version
		if err := item.After.Doc.Apply(beforeVersion, edits); err != nil {
			return err
		}
		if item.After.Doc.Text.Len() > workspaceBytes {
			return errors.New("workspace result exceeds 8 MiB")
		}
		if item.After.Doc.Version != beforeVersion {
			item.Forward = append(item.Forward, item.After.Doc.LastChanges...)
			d := item.After.Doc.Clone()
			d.Undo()
			item.Reverse = append(d.LastChanges, item.Reverse...)
		}
		return nil
	}
	removeBuffers := func(path string) error {
		for at, item := range work {
			if fileops.Inside(path, at) {
				if err := touch(item); err != nil {
					return err
				}
				item.After.File = File{}
				item.After.Title = "Deleted: " + filepath.Base(at)
				item.After.Transient = true
				item.After.Doc.MarkSaved()
				delete(work, at)
			}
		}
		return nil
	}
	for i, raw := range wire.DocumentChanges {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		var c resourceChange
		if err = json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		if err = annotate(c.AnnotationID); err != nil {
			return nil, err
		}
		for _, edit := range c.Edits {
			if err = annotate(edit.AnnotationID); err != nil {
				return nil, err
			}
		}
		pathURI := c.URI
		if c.Kind == "" {
			pathURI = c.TextDocument.URI
		} else if c.Kind == "rename" {
			pathURI = c.OldURI
		}
		path, e := lsp.Path(pathURI)
		if e != nil {
			return nil, e
		}
		path, e = plan.Path(path)
		if e != nil {
			return nil, e
		}
		rel, _ := filepath.Rel(plan.Root, path)
		operation := func() error {
			switch c.Kind {
			case "":
				item, e := load(path)
				if e != nil {
					return e
				}
				if c.TextDocument.Version != nil && (item.New || *c.TextDocument.Version != item.Before.Doc.Version) {
					return errors.New("language server returned a stale resource document version")
				}
				source := item.After.Doc.Text.String()
				edits, e := lsp.Edits(source, c.Edits)
				if e != nil {
					return e
				}
				for _, edit := range edits {
					if e = row("Edit "+rel, source[edit.Start:edit.End], edit.Text); e != nil {
						return e
					}
				}
				return apply(item, edits)
			case "create":
				old, e := plan.Read(path)
				if e != nil {
					return e
				}
				changed, e := plan.Create(path, c.Options.Overwrite, c.Options.IgnoreIfExists)
				if e != nil {
					return e
				}
				if !changed {
					return row("Create "+rel, "Already exists", "Ignored (ignoreIfExists)")
				}
				item := work[path]
				if item != nil {
					if e = touch(item); e != nil {
						return e
					}
					if e = apply(item, []text.Edit{{Start: 0, End: item.After.Doc.Text.Len(), Text: ""}}); e != nil {
						return e
					}
				} else {
					d, _ := text.NewDocument("")
					b := &Buffer{}
					before := resourceSnapshot{Buffer: b, File: File{}, Doc: d, Title: "Undone creation: " + rel, Transient: true, Newline: "\n"}
					item = &resourceBuffer{Before: before, After: resourceSnapshot{Buffer: b, File: File{Path: path}, Doc: d.Clone(), Newline: "\n"}, New: true}
					touched[item] = true
					result.Buffers = append(result.Buffers, item)
					work[path] = item
				}
				item.After.Doc.MarkSaved()
				return row("Create "+rel, resourceSummary(old), "Empty file (subsequent text edits are shown separately)")
			case "rename":
				to, e := lsp.Path(c.NewURI)
				if e != nil {
					return e
				}
				to, e = plan.Path(to)
				if e != nil {
					return e
				}
				destination, e := plan.Read(to)
				if e != nil {
					return e
				}
				sourceTree, e := plan.Tree(path)
				if e != nil {
					return e
				}
				var replaced map[string]fileops.Node
				if destination.Kind == fileops.Directory && c.Options.Overwrite {
					replaced, e = plan.Tree(to)
					if e != nil {
						return e
					}
				}
				changed, e := plan.Rename(path, to, c.Options.Overwrite, c.Options.IgnoreIfExists)
				if e != nil {
					return e
				}
				newRel, _ := filepath.Rel(plan.Root, to)
				if !changed {
					return row("Rename "+rel+" → "+newRel, "", "Ignored / unchanged")
				}
				originUpdates := map[string]string{}
				for next, n := range plan.After {
					if n.Kind == fileops.File && fileops.Inside(to, next) {
						suffix, _ := filepath.Rel(to, next)
						old := filepath.Join(path, suffix)
						origin, ok := origins[old]
						if !ok {
							origin = old
						}
						originUpdates[next] = origin
					}
				}
				for at := range origins {
					if fileops.Inside(path, at) || fileops.Inside(to, at) {
						delete(origins, at)
					}
				}
				for at, origin := range originUpdates {
					origins[at] = origin
				}
				if e = removeBuffers(to); e != nil {
					return e
				}
				moving := map[string]*resourceBuffer{}
				for at, item := range work {
					if fileops.Inside(path, at) {
						if e = touch(item); e != nil {
							return e
						}
						suffix, _ := filepath.Rel(path, at)
						next := filepath.Join(to, suffix)
						item.After.File.Path = next
						moving[next] = item
						delete(work, at)
					}
				}
				for at, item := range moving {
					work[at] = item
				}
				if e = contents("Overwrite destination", replaced); e != nil {
					return e
				}
				names := []string{}
				for source := range sourceTree {
					suffix, _ := filepath.Rel(path, source)
					names = append(names, filepath.Join(rel, suffix)+" → "+filepath.Join(newRel, suffix))
				}
				sort.Strings(names)
				return row("Rename "+rel+" → "+newRel, "Source: "+rel+"\nDestination: "+resourceSummary(destination), strings.Join(names, "\n")+"\nOpen buffer edits follow the new path")
			case "delete":
				old, e := plan.Read(path)
				if e != nil {
					return e
				}
				var removed map[string]fileops.Node
				if old.Kind == fileops.Directory {
					removed, e = plan.Tree(path)
					if e != nil {
						return e
					}
				}
				changed, e := plan.Delete(path, c.Options.Recursive, c.Options.IgnoreIfNotExists)
				if e != nil {
					return e
				}
				if !changed {
					return row("Delete "+rel, "Missing", "Ignored (ignoreIfNotExists)")
				}
				if e = removeBuffers(path); e != nil {
					return e
				}
				if e = contents("Delete", removed); e != nil {
					return e
				}
				return row("Delete "+rel, resourceSummary(old), "Delete from disk; any open text is retained as a read-only report")
			default:
				return fmt.Errorf("unsupported resource operation %q", c.Kind)
			}
		}
		if err = operation(); err != nil {
			return nil, fmt.Errorf("workspace operation %d: %w", i+1, err)
		}
	}
	// Include missing paths of unsaved buffers inside a renamed directory in
	// conflict validation, even though no corresponding file exists on disk yet.
	for path, item := range work {
		if !touched[item] {
			continue
		}
		n, e := plan.Read(path)
		if e != nil {
			return nil, e
		}
		item.After.File = File{Path: path, exists: n.Kind == fileops.File, hash: sha256.Sum256([]byte(n.Data))}
		if n.Kind == fileops.File && item.After.Doc.Text.String() == n.Data {
			item.After.Doc.MarkSaved()
		}
	}
	result.Before, result.After = plan.Images()
	if !result.HasFiles {
		for _, item := range result.Buffers {
			d := item.Before.Doc.Clone()
			if err := d.ApplySequence(d.Version, item.Forward); err != nil {
				return nil, err
			}
			item.After.Doc = d
		}
	}
	// Closed files loaded after a rename should revert to their original path.
	// Pure disk operations remain represented by the filesystem images regardless.
	sort.SliceStable(result.Buffers, func(i, j int) bool { return result.Buffers[i].Before.File.Path < result.Buffers[j].Before.File.Path })
	return result, nil
}

func resourceSummary(n fileops.Node) string {
	switch n.Kind {
	case fileops.Missing:
		return "Missing"
	case fileops.Directory:
		return "Directory (recursive contents included in the operation)"
	default:
		if !utf8.ValidString(n.Data) || strings.IndexByte(n.Data, 0) >= 0 {
			return fmt.Sprintf("Binary file (%d bytes)", len(n.Data))
		}
		return n.Data
	}
}
