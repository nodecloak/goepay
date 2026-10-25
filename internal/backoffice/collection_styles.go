package backoffice

import (
	"context"
	"encoding/json"
	"goepay/internal/collectionstyle"
	"strconv"
)

type CollectionStyle struct {
	ID       string                  `json:"id"`
	Revision string                  `json:"revision"`
	Style    *collectionstyle.Layout `json:"style"`
	Fields   map[string]any          `json:"fields"`
}

func StyleItem(item Item) (CollectionStyle, error) {
	l, e := collectionstyle.Parse(item.Fields)
	if e != nil {
		return CollectionStyle{}, ErrInvalid
	}
	return CollectionStyle{ID: strconv.FormatInt(item.ID, 10), Revision: collectionstyle.Revision(item.Fields), Style: l, Fields: item.Fields}, nil
}
func (s *Service) CollectionStyles(ctx context.Context, p Principal) ([]CollectionStyle, error) {
	if p.SubjectID < 1 || (p.Role != "admin" && p.Role != "merchant") {
		return nil, ErrForbidden
	}
	rows, e := s.pool.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='collection-styles' AND ($1::text='admin' OR body->>'enabled'='true') ORDER BY COALESCE(CASE WHEN body->>'sort'~'^[0-9]{1,7}$' THEN (body->>'sort')::integer END,0),id`, p.Role)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []CollectionStyle{}
	for rows.Next() {
		var id int64
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			return nil, e
		}
		item, e := StyleItem(Item{ID: id, Fields: decodeFields(raw)})
		if e != nil {
			return nil, e
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Service) UpdateCollectionStyle(ctx context.Context, p Principal, id int64, expected string, in map[string]json.RawMessage) (Item, error) {
	if len(expected) != 64 {
		return Item{}, ErrInvalid
	}
	return s.update(ctx, p, "collection-styles", id, in, expected)
}
