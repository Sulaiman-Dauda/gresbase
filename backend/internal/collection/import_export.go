package collection

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/gresbase/gresbase/internal/database"
)

// ExportCollections returns a snapshot-friendly representation of all collections.
func (s *Service) ExportCollections(ctx context.Context) ([]map[string]any, error) {
	collections, err := s.ListCollections(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(collections))
	for _, coll := range collections {
		raw, err := json.Marshal(coll)
		if err != nil {
			return nil, err
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	return result, nil
}

// ImportCollections imports collection snapshots.
// Existing collections are matched by id first and then by name.
func (s *Service) ImportCollections(ctx context.Context, toImport []map[string]any, deleteMissing bool) error {
	if len(toImport) == 0 {
		return errors.New("no collections to import")
	}

	return s.db.RunInTransactionContext(ctx, func(txCtx context.Context, tx database.Tx) error {
		existing, err := s.ListCollections(txCtx)
		if err != nil {
			return err
		}

		byID := make(map[string]*Collection, len(existing))
		byName := make(map[string]*Collection, len(existing))
		for _, coll := range existing {
			byID[coll.ID] = coll
			byName[coll.Name] = coll
		}

		imported := make([]*Collection, 0, len(toImport))
		keep := make(map[string]bool, len(toImport))

		type importedCollection struct {
			Collection *Collection
			Existing   bool
		}

		imports := make([]importedCollection, 0, len(toImport))
		for _, item := range toImport {
			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			coll := &Collection{}
			if err := json.Unmarshal(raw, coll); err != nil {
				return err
			}
			if err := s.ValidateCollectionDefinition(coll); err != nil {
				return err
			}

			matched := false
			if coll.ID != "" {
				if existing := byID[coll.ID]; existing != nil {
					coll.ID = existing.ID
					keep[existing.ID] = true
					matched = true
				}
			}
			if !matched && coll.Name != "" {
				if existing := byName[coll.Name]; existing != nil {
					coll.ID = existing.ID
					keep[existing.ID] = true
					matched = true
				}
			}
			if coll.ID != "" {
				keep[coll.ID] = true
			}

			imported = append(imported, coll)
			imports = append(imports, importedCollection{Collection: coll, Existing: matched})
		}

		sort.SliceStable(imported, func(i, j int) bool {
			if imported[i].Type == TypeView && imported[j].Type != TypeView {
				return false
			}
			if imported[i].Type != TypeView && imported[j].Type == TypeView {
				return true
			}
			return imported[i].Name < imported[j].Name
		})

		sortedImports := make([]importedCollection, 0, len(imported))
		for _, coll := range imported {
			for i, item := range imports {
				if item.Collection == coll {
					sortedImports = append(sortedImports, item)
					imports = append(imports[:i], imports[i+1:]...)
					break
				}
			}
		}

		if deleteMissing {
			for _, coll := range existing {
				if coll.System {
					continue
				}
				if keep[coll.ID] || keep[coll.Name] {
					continue
				}
				if err := s.DeleteCollection(txCtx, coll.ID); err != nil {
					return err
				}
			}
		}

		for _, item := range sortedImports {
			if item.Existing {
				if err := s.UpdateCollection(txCtx, item.Collection); err != nil {
					return err
				}
				continue
			}
			if err := s.CreateCollection(txCtx, item.Collection); err != nil {
				return err
			}
		}

		return nil
	})
}
