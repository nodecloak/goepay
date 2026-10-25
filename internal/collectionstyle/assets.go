package collectionstyle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxImageInput = 1 << 20
const MaxImageOutput = 4 << 20

type Asset struct {
	ID     string `json:"id"`
	MIME   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  []byte `json:"-"`
}
type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(raw []byte) (int, error) {
	if len(raw) > MaxImageOutput-b.Len() {
		return 0, ErrInvalid
	}
	return b.Buffer.Write(raw)
}
func NormalizeImage(raw []byte) (Asset, error) {
	var a Asset
	if len(raw) < 1 || len(raw) > MaxImageInput {
		return a, ErrInvalid
	}
	cfg, kind, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (kind != "png" && kind != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 4194304 {
		return a, ErrInvalid
	}
	img, decoded, e := image.Decode(bytes.NewReader(raw))
	if e != nil || decoded != kind || img.Bounds().Dx() != cfg.Width || img.Bounds().Dy() != cfg.Height {
		return a, ErrInvalid
	}
	var b boundedBuffer
	if kind == "png" {
		e = png.Encode(&b, img)
	} else {
		e = jpeg.Encode(&b, img, &jpeg.Options{Quality: 85})
	}
	if e != nil {
		return Asset{}, ErrInvalid
	}
	hash := sha256.Sum256(b.Bytes())
	return Asset{ID: hex.EncodeToString(hash[:]), MIME: "image/" + kind, Width: cfg.Width, Height: cfg.Height, Bytes: b.Bytes()}, nil
}
func Upload(ctx context.Context, pool *pgxpool.Pool, raw []byte, actorID int64, actorName string) (Asset, error) {
	if actorID < 1 || actorName == "" {
		return Asset{}, ErrInvalid
	}
	a, e := NormalizeImage(raw)
	if e != nil {
		return a, e
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		return Asset{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `INSERT INTO collection_style_assets(id,mime,width,height,content) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO NOTHING`, a.ID, a.MIME, a.Width, a.Height, a.Bytes); e != nil {
		return Asset{}, e
	}
	changes, _ := json.Marshal(map[string]any{"image_id": a.ID, "width": a.Width, "height": a.Height})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'collection-style-images',0,'upload-image',$3)`, actorID, actorName, changes); e != nil {
		return Asset{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Asset{}, e
	}
	return ReadAsset(ctx, pool, a.ID, false)
}
func ReadAsset(ctx context.Context, pool *pgxpool.Pool, id string, published bool) (Asset, error) {
	var a Asset
	if AssetID("/collection-style-assets/"+id) != id || id == "" {
		return a, ErrInvalid
	}
	if published {
		var active bool
		e := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='collection-styles' AND body->>'enabled'='true' AND body->>'url'=$1)`, "/collection-style-assets/"+id).Scan(&active)
		if e != nil {
			return a, e
		}
		if !active {
			return a, ErrInvalid
		}
	}
	e := pool.QueryRow(ctx, `SELECT id,mime,width,height,content FROM collection_style_assets WHERE id=$1`, id).Scan(&a.ID, &a.MIME, &a.Width, &a.Height, &a.Bytes)
	if e != nil {
		return a, e
	}
	hash := sha256.Sum256(a.Bytes)
	cfg, kind, e := image.DecodeConfig(bytes.NewReader(a.Bytes))
	if e != nil || hex.EncodeToString(hash[:]) != a.ID || (kind != "png" && kind != "jpeg") || a.MIME != "image/"+kind || cfg.Width != a.Width || cfg.Height != a.Height || len(a.Bytes) > MaxImageOutput || a.Width < 1 || a.Height < 1 || a.Width > 4096 || a.Height > 4096 || int64(a.Width)*int64(a.Height) > 4194304 {
		return Asset{}, ErrInvalid
	}
	return a, nil
}
