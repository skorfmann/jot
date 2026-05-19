package server

import (
	"context"
	"time"
)

const trashTTL = 7 * 24 * time.Hour

func (s *Server) runGC(ctx context.Context) error {
	manifests, err := s.store.ListAllManifests(ctx)
	if err != nil {
		return err
	}
	referenced := map[string]struct{}{}
	for _, m := range manifests {
		for _, f := range m.Files {
			referenced[f.SHA256] = struct{}{}
		}
	}
	blobs, err := s.store.ListBlobHashes(ctx)
	if err != nil {
		return err
	}
	moved := 0
	for hash := range blobs {
		if _, ok := referenced[hash]; ok {
			continue
		}
		if err := s.store.MoveBlobToTrash(ctx, hash); err != nil {
			return err
		}
		moved++
	}
	if err := s.store.DeleteExpiredTrash(ctx, trashTTL); err != nil {
		return err
	}
	if moved > 0 {
		s.log.Info("gc moved unreferenced blobs to trash", "event_type", "gc", "count", moved)
	}
	return nil
}
