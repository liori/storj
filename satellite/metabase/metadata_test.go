// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

package metabase_test

import (
	"testing"
	"time"

	"storj.io/common/testcontext"
	"storj.io/common/testrand"
	"storj.io/common/uuid"
	"storj.io/storj/satellite/metabase"
	"storj.io/storj/satellite/metabase/metabasetest"
)

func TestUpdateObjectLastCommittedMetadata(t *testing.T) {
	metabasetest.Run(t, func(ctx *testcontext.Context, t *testing.T, db *metabase.DB) {
		obj := metabasetest.RandObjectStream()
		for _, test := range metabasetest.InvalidObjectLocations(obj.Location()) {
			test := test
			t.Run(test.Name, func(t *testing.T) {
				defer metabasetest.DeleteAll{}.Check(ctx, t, db)
				metabasetest.UpdateObjectLastCommittedMetadata{
					Opts: metabase.UpdateObjectLastCommittedMetadata{
						ProjectID:  test.ObjectLocation.ProjectID,
						BucketName: test.ObjectLocation.BucketName,
						ObjectKey:  test.ObjectLocation.ObjectKey,
					},
					ErrClass: test.ErrClass,
					ErrText:  test.ErrText,
				}.Check(ctx, t, db)
				metabasetest.Verify{}.Check(ctx, t, db)
			})
		}

		t.Run("StreamID missing", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:  obj.ProjectID,
					BucketName: obj.BucketName,
					ObjectKey:  obj.ObjectKey,
					StreamID:   uuid.UUID{},
				},
				ErrClass: &metabase.ErrInvalidRequest,
				ErrText:  "StreamID missing",
			}.Check(ctx, t, db)
			metabasetest.Verify{}.Check(ctx, t, db)
		})

		t.Run("Metadata missing", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:  obj.ProjectID,
					BucketName: obj.BucketName,
					ObjectKey:  obj.ObjectKey,
					StreamID:   obj.StreamID,
				},
				ErrClass: &metabase.ErrObjectNotFound,
				ErrText:  "object with specified version and committed status is missing",
			}.Check(ctx, t, db)
			metabasetest.Verify{}.Check(ctx, t, db)
		})

		t.Run("Update metadata", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			object := metabasetest.CreateObject(ctx, t, db, obj, 0)

			encryptedMetadata := testrand.Bytes(1024)
			encryptedMetadataNonce := testrand.Nonce()
			encryptedMetadataKey := testrand.Bytes(265)

			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     obj.ProjectID,
					BucketName:                    obj.BucketName,
					ObjectKey:                     obj.ObjectKey,
					StreamID:                      obj.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
			}.Check(ctx, t, db)

			object.EncryptedMetadata = encryptedMetadata
			object.EncryptedMetadataNonce = encryptedMetadataNonce[:]
			object.EncryptedMetadataEncryptedKey = encryptedMetadataKey

			metabasetest.Verify{
				Objects: []metabase.RawObject{
					metabase.RawObject(object),
				},
			}.Check(ctx, t, db)
		})

		t.Run("Update metadata with version != 1", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			object := metabasetest.CreatePendingObject(ctx, t, db, obj, 0)

			obj2 := obj
			obj2.Version++
			object2 := metabasetest.CreateObject(ctx, t, db, obj2, 0)

			encryptedMetadata := testrand.Bytes(1024)
			encryptedMetadataNonce := testrand.Nonce()
			encryptedMetadataKey := testrand.Bytes(265)

			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     object2.ProjectID,
					BucketName:                    object2.BucketName,
					ObjectKey:                     object2.ObjectKey,
					StreamID:                      object2.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
			}.Check(ctx, t, db)

			object2.EncryptedMetadata = encryptedMetadata
			object2.EncryptedMetadataNonce = encryptedMetadataNonce[:]
			object2.EncryptedMetadataEncryptedKey = encryptedMetadataKey

			metabasetest.Verify{
				Objects: []metabase.RawObject{
					metabase.RawObject(object),
					metabase.RawObject(object2),
				},
			}.Check(ctx, t, db)
		})

		t.Run("update metadata of versioned object", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			object := metabasetest.CreateObjectVersioned(ctx, t, db, obj, 0)

			encryptedMetadata := testrand.Bytes(1024)
			encryptedMetadataNonce := testrand.Nonce()
			encryptedMetadataKey := testrand.Bytes(265)

			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     object.ProjectID,
					BucketName:                    object.BucketName,
					ObjectKey:                     object.ObjectKey,
					StreamID:                      object.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
			}.Check(ctx, t, db)

			object.EncryptedMetadata = encryptedMetadata
			object.EncryptedMetadataNonce = encryptedMetadataNonce[:]
			object.EncryptedMetadataEncryptedKey = encryptedMetadataKey

			metabasetest.Verify{
				Objects: []metabase.RawObject{
					metabase.RawObject(object),
				},
			}.Check(ctx, t, db)
		})

		t.Run("update metadata of versioned delete marker", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			object := metabasetest.CreateObjectVersioned(ctx, t, db, obj, 0)

			encryptedMetadata := testrand.Bytes(1024)
			encryptedMetadataNonce := testrand.Nonce()
			encryptedMetadataKey := testrand.Bytes(265)

			marker := metabase.Object{
				ObjectStream: object.ObjectStream,
				Status:       metabase.DeleteMarkerVersioned,
				CreatedAt:    time.Now(),
			}
			marker.StreamID = uuid.UUID{}
			marker.Version++

			metabasetest.DeleteObjectLastCommitted{
				Opts: metabase.DeleteObjectLastCommitted{
					ObjectLocation: object.Location(),
					Versioned:      true,
				},
				Result: metabase.DeleteObjectResult{
					Markers: []metabase.Object{marker},
				},
			}.Check(ctx, t, db)

			// verify we cannot update the metadata of a deleted object
			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     object.ProjectID,
					BucketName:                    object.BucketName,
					ObjectKey:                     object.ObjectKey,
					StreamID:                      object.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
				ErrClass: &metabase.ErrObjectNotFound,
				ErrText:  "object with specified version and committed status is missing",
			}.Check(ctx, t, db)

			// verify cannot update the metadata of the delete marker either
			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     marker.ProjectID,
					BucketName:                    marker.BucketName,
					ObjectKey:                     marker.ObjectKey,
					StreamID:                      marker.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
				ErrClass: &metabase.ErrInvalidRequest,
				ErrText:  "StreamID missing",
			}.Check(ctx, t, db)

			metabasetest.Verify{
				Objects: []metabase.RawObject{
					metabase.RawObject(object),
					metabase.RawObject(marker),
				},
			}.Check(ctx, t, db)
		})

		t.Run("update metadata of unversioned delete marker", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			object := metabasetest.CreateObjectVersioned(ctx, t, db, obj, 0)

			obj2 := obj
			obj2.Version++

			object2 := metabasetest.CreateObject(ctx, t, db, obj2, 0)

			marker := metabase.Object{
				ObjectStream: object2.ObjectStream,
				Status:       metabase.DeleteMarkerUnversioned,
				CreatedAt:    time.Now(),
			}
			marker.Version++
			marker.StreamID = uuid.UUID{}

			metabasetest.DeleteObjectLastCommitted{
				Opts: metabase.DeleteObjectLastCommitted{
					ObjectLocation: object2.Location(),
					Versioned:      false,
					Suspended:      true,
				},
				Result: metabase.DeleteObjectResult{
					Markers: []metabase.Object{marker},
					Removed: []metabase.Object{object2},
				},
			}.Check(ctx, t, db)

			encryptedMetadata := testrand.Bytes(1024)
			encryptedMetadataNonce := testrand.Nonce()
			encryptedMetadataKey := testrand.Bytes(265)

			// verify we cannot update the metadata of a deleted object
			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     object2.ProjectID,
					BucketName:                    object2.BucketName,
					ObjectKey:                     object2.ObjectKey,
					StreamID:                      object2.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
				ErrClass: &metabase.ErrObjectNotFound,
				ErrText:  "object with specified version and committed status is missing",
			}.Check(ctx, t, db)

			// verify cannot update the metadata of the delete marker either
			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     marker.ProjectID,
					BucketName:                    marker.BucketName,
					ObjectKey:                     marker.ObjectKey,
					StreamID:                      marker.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
				ErrClass: &metabase.ErrInvalidRequest,
				ErrText:  "StreamID missing",
			}.Check(ctx, t, db)

			metabasetest.Verify{
				Objects: []metabase.RawObject{
					metabase.RawObject(object),
					metabase.RawObject(marker),
				},
			}.Check(ctx, t, db)
		})

		t.Run("update metadata of versioned object with previous delete marker", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			object := metabasetest.CreateObjectVersioned(ctx, t, db, obj, 0)

			marker := metabase.Object{
				ObjectStream: object.ObjectStream,
				Status:       metabase.DeleteMarkerVersioned,
				CreatedAt:    time.Now(),
			}
			marker.StreamID = uuid.UUID{}
			marker.Version++

			metabasetest.DeleteObjectLastCommitted{
				Opts: metabase.DeleteObjectLastCommitted{
					ObjectLocation: object.Location(),
					Versioned:      true,
				},
				Result: metabase.DeleteObjectResult{
					Markers: []metabase.Object{marker},
				},
			}.Check(ctx, t, db)

			obj2 := obj
			obj2.StreamID = testrand.UUID()
			obj2.Version = marker.Version + 1
			object2 := metabasetest.CreateObjectVersioned(ctx, t, db, obj2, 0)

			encryptedMetadata := testrand.Bytes(1024)
			encryptedMetadataNonce := testrand.Nonce()
			encryptedMetadataKey := testrand.Bytes(265)

			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     object2.ProjectID,
					BucketName:                    object2.BucketName,
					ObjectKey:                     object2.ObjectKey,
					StreamID:                      object2.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
			}.Check(ctx, t, db)

			object2.EncryptedMetadata = encryptedMetadata
			object2.EncryptedMetadataNonce = encryptedMetadataNonce[:]
			object2.EncryptedMetadataEncryptedKey = encryptedMetadataKey

			metabasetest.Verify{
				Objects: []metabase.RawObject{
					metabase.RawObject(object),
					metabase.RawObject(marker),
					metabase.RawObject(object2),
				},
			}.Check(ctx, t, db)
		})

		t.Run("update metadata of unversioned object with previous version", func(t *testing.T) {
			defer metabasetest.DeleteAll{}.Check(ctx, t, db)

			obj := metabasetest.RandObjectStream()
			object := metabasetest.CreateObjectVersioned(ctx, t, db, obj, 0)

			obj2 := obj
			obj2.StreamID = testrand.UUID()
			obj2.Version = obj.Version + 1
			object2 := metabasetest.CreateObjectVersioned(ctx, t, db, obj2, 0)

			obj3 := obj
			obj3.StreamID = testrand.UUID()
			obj3.Version = obj2.Version + 1
			object3 := metabasetest.CreateObject(ctx, t, db, obj3, 0)

			encryptedMetadata := testrand.Bytes(1024)
			encryptedMetadataNonce := testrand.Nonce()
			encryptedMetadataKey := testrand.Bytes(265)

			metabasetest.UpdateObjectLastCommittedMetadata{
				Opts: metabase.UpdateObjectLastCommittedMetadata{
					ProjectID:                     object3.ProjectID,
					BucketName:                    object3.BucketName,
					ObjectKey:                     object3.ObjectKey,
					StreamID:                      object3.StreamID,
					EncryptedMetadata:             encryptedMetadata,
					EncryptedMetadataNonce:        encryptedMetadataNonce[:],
					EncryptedMetadataEncryptedKey: encryptedMetadataKey,
				},
			}.Check(ctx, t, db)

			object3.EncryptedMetadata = encryptedMetadata
			object3.EncryptedMetadataNonce = encryptedMetadataNonce[:]
			object3.EncryptedMetadataEncryptedKey = encryptedMetadataKey

			metabasetest.Verify{
				Objects: []metabase.RawObject{
					metabase.RawObject(object),
					metabase.RawObject(object2),
					metabase.RawObject(object3),
				},
			}.Check(ctx, t, db)
		})
	})
}
