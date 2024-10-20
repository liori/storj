// Copyright (C) 2023 Storj Labs, Inc.
// See LICENSE for copying information.

package pieces

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/common/bloomfilter"
	"storj.io/common/storj"
	"storj.io/storj/storagenode/blobstore"
	"storj.io/storj/storagenode/blobstore/filestore"
)

var errFileWalker = errs.Class("filewalker")

// FileWalker implements methods to walk over pieces in a storage directory.
type FileWalker struct {
	log *zap.Logger

	blobs       blobstore.Blobs
	v0PieceInfo V0PieceInfoDB
}

// NewFileWalker creates a new FileWalker.
func NewFileWalker(log *zap.Logger, blobs blobstore.Blobs, db V0PieceInfoDB) *FileWalker {
	return &FileWalker{
		log:         log,
		blobs:       blobs,
		v0PieceInfo: db,
	}
}

// WalkSatellitePieces executes walkFunc for each locally stored piece in the namespace of the
// given satellite. If walkFunc returns a non-nil error, WalkSatellitePieces will stop iterating
// and return the error immediately. The ctx parameter is intended specifically to allow canceling
// iteration early.
//
// Note that this method includes all locally stored pieces, both V0 and higher.
func (fw *FileWalker) WalkSatellitePieces(ctx context.Context, satellite storj.NodeID, fn func(StoredPieceAccess) error) (err error) {
	defer mon.Task()(&ctx)(&err)
	// iterate over all in V1 storage, skipping v0 pieces
	err = fw.blobs.WalkNamespace(ctx, satellite.Bytes(), func(blobInfo blobstore.BlobInfo) error {
		if blobInfo.StorageFormatVersion() < filestore.FormatV1 {
			// skip v0 pieces, which are handled separately
			return nil
		}
		pieceAccess, err := newStoredPieceAccess(fw.blobs, blobInfo)
		if err != nil {
			// this is not a real piece blob. the blob store can't distinguish between actual piece
			// blobs and stray files whose names happen to decode as valid base32. skip this
			// "blob".
			return nil //nolint: nilerr // we ignore other files
		}
		return fn(pieceAccess)
	})

	if err == nil && fw.v0PieceInfo != nil {
		// iterate over all in V0 storage
		err = fw.v0PieceInfo.WalkSatelliteV0Pieces(ctx, fw.blobs, satellite, fn)
	}

	return errFileWalker.Wrap(err)
}

// WalkAndComputeSpaceUsedBySatellite walks over all pieces for a given satellite, adds up and returns the total space used.
func (fw *FileWalker) WalkAndComputeSpaceUsedBySatellite(ctx context.Context, satelliteID storj.NodeID) (satPiecesTotal int64, satPiecesContentSize int64, err error) {
	err = fw.WalkSatellitePieces(ctx, satelliteID, func(access StoredPieceAccess) error {
		pieceTotal, pieceContentSize, err := access.Size(ctx)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		satPiecesTotal += pieceTotal
		satPiecesContentSize += pieceContentSize
		return nil
	})

	return satPiecesTotal, satPiecesContentSize, errFileWalker.Wrap(err)
}

// WalkSatellitePiecesToTrash returns a list of piece IDs that need to be trashed for the given satellite.
//
// ------------------------------------------------------------------------------------------------
//
// On the correctness of using access.ModTime() in place of the more precise access.CreationTime():
//
// ------------------------------------------------------------------------------------------------
//
// Background: for pieces not stored with storage.FormatV0, the access.CreationTime() value can
// only be retrieved by opening the piece file, and reading and unmarshaling the piece header.
// This is far slower than access.ModTime(), which gets the file modification time from the file
// system and only needs to do a stat(2) on the piece file. If we can make Retain() work with
// ModTime, we should.
//
// Possibility of mismatch: We do not force or require piece file modification times to be equal to
// or close to the CreationTime specified by the uplink, but we do expect that piece files will be
// written to the filesystem _after_ the CreationTime. We make the assumption already that storage
// nodes and satellites and uplinks have system clocks that are very roughly in sync (that is, they
// are out of sync with each other by less than an hour of real time, or whatever is configured as
// MaxTimeSkew). So if an uplink is not lying about CreationTime and it uploads a piece that
// makes it to a storagenode's disk as quickly as possible, even in the worst-synchronized-clocks
// case we can assume that `ModTime > (CreationTime - MaxTimeSkew)`. We also allow for storage
// node operators doing file system manipulations after a piece has been written. If piece files
// are copied between volumes and their attributes are not preserved, it will be possible for their
// modification times to be changed to something later in time. This still preserves the inequality
// relationship mentioned above, `ModTime > (CreationTime - MaxTimeSkew)`. We only stipulate
// that storage node operators must not artificially change blob file modification times to be in
// the past.
//
// If there is a mismatch: in most cases, a mismatch between ModTime and CreationTime has no
// effect. In certain remaining cases, the only effect is that a piece file which _should_ be
// garbage collected survives until the next round of garbage collection. The only really
// problematic case is when there is a relatively new piece file which was created _after_ this
// node's Retain bloom filter started being built on the satellite, and is recorded in this
// storage node's blob store before the Retain operation has completed. Then, it might be possible
// for that new piece to be garbage collected incorrectly, because it does not show up in the
// bloom filter and the node incorrectly thinks that it was created before the bloom filter.
// But if the uplink is not lying about CreationTime and its clock drift versus the storage node
// is less than `MaxTimeSkew`, and the ModTime on a blob file is correctly set from the
// storage node system time, then it is still true that `ModTime > (CreationTime -
// MaxTimeSkew)`.
//
// The rule that storage node operators need to be aware of is only this: do not artificially set
// mtimes on blob files to be in the past. Let the filesystem manage mtimes. If blob files need to
// be moved or copied between locations, and this updates the mtime, that is ok. A secondary effect
// of this rule is that if the storage node's system clock needs to be changed forward by a
// nontrivial amount, mtimes on existing blobs should also be adjusted (by the same interval,
// ideally, but just running "touch" on all blobs is sufficient to avoid incorrect deletion of
// data).
func (fw *FileWalker) WalkSatellitePiecesToTrash(ctx context.Context, satelliteID storj.NodeID, createdBefore time.Time, filter *bloomfilter.Filter) (pieceIDs []storj.PieceID, piecesCount, piecesSkipped int64, err error) {
	defer mon.Task()(&ctx)(&err)

	if filter == nil {
		return
	}

	err = fw.WalkSatellitePieces(ctx, satelliteID, func(access StoredPieceAccess) error {
		piecesCount++

		// We call Gosched() when done because the GC process is expected to be long and we want to keep it at low priority,
		// so other goroutines can continue serving requests.
		defer runtime.Gosched()

		pieceID := access.PieceID()
		if filter.Contains(pieceID) {
			// This piece is explicitly not trash. Move on.
			return nil
		}

		// If the blob's mtime is at or after the createdBefore line, we can't safely delete it;
		// it might not be trash. If it is, we can expect to get it next time.
		//
		// See the comment above the WalkSatellitePiecesToTrash() function for a discussion on the correctness
		// of using ModTime in place of the more precise CreationTime.
		mTime, err := access.ModTime(ctx)
		if err != nil {
			if os.IsNotExist(err) {
				// piece was deleted while we were scanning.
				return nil
			}

			piecesSkipped++
			fw.log.Warn("failed to determine mtime of blob", zap.Error(err))
			// but continue iterating.
			return nil
		}
		if !mTime.Before(createdBefore) {
			return nil
		}

		pieceIDs = append(pieceIDs, pieceID)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		return nil
	})

	return pieceIDs, piecesCount, piecesSkipped, errFileWalker.Wrap(err)
}
