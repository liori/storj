// Copyright (C) 2022 Storj Labs, Inc.
// See LICENSE for copying information.

package satellite

import (
	"context"
	"errors"
	"net"
	"runtime/pprof"

	"github.com/spacemonkeygo/monkit/v3"
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"storj.io/common/peertls/extensions"
	"storj.io/private/debug"
	"storj.io/private/version"
	"storj.io/storj/private/lifecycle"
	"storj.io/storj/satellite/gc/bloomfilter"
	"storj.io/storj/satellite/metabase"
	"storj.io/storj/satellite/metabase/rangedloop"
	"storj.io/storj/satellite/metabase/segmentloop"
	"storj.io/storj/satellite/overlay"
)

// GarbageCollectionBF is the satellite garbage collection process which collects bloom filters.
//
// architecture: Peer
type GarbageCollectionBF struct {
	Log *zap.Logger
	DB  DB

	Servers  *lifecycle.Group
	Services *lifecycle.Group

	Debug struct {
		Listener net.Listener
		Server   *debug.Server
	}

	Overlay struct {
		DB overlay.DB
	}

	Metainfo struct {
		SegmentLoop *segmentloop.Service
	}

	GarbageCollection struct {
		Config  bloomfilter.Config
		Service *bloomfilter.Service
	}

	RangedLoop struct {
		Service *rangedloop.Service
	}
}

// NewGarbageCollectionBF creates a new satellite garbage collection peer which collects storage nodes bloom filters.
func NewGarbageCollectionBF(log *zap.Logger, db DB, metabaseDB *metabase.DB, revocationDB extensions.RevocationDB,
	versionInfo version.Info, config *Config, atomicLogLevel *zap.AtomicLevel) (*GarbageCollectionBF, error) {
	peer := &GarbageCollectionBF{
		Log: log,
		DB:  db,

		Servers:  lifecycle.NewGroup(log.Named("servers")),
		Services: lifecycle.NewGroup(log.Named("services")),
	}

	{ // setup debug
		var err error
		if config.Debug.Address != "" {
			peer.Debug.Listener, err = net.Listen("tcp", config.Debug.Address)
			if err != nil {
				withoutStack := errors.New(err.Error())
				peer.Log.Debug("failed to start debug endpoints", zap.Error(withoutStack))
			}
		}
		debugConfig := config.Debug
		debugConfig.ControlTitle = "GC-BloomFilter"
		peer.Debug.Server = debug.NewServerWithAtomicLevel(log.Named("debug"), peer.Debug.Listener, monkit.Default, debugConfig, atomicLogLevel)
		peer.Servers.Add(lifecycle.Item{
			Name:  "debug",
			Run:   peer.Debug.Server.Run,
			Close: peer.Debug.Server.Close,
		})
	}

	{ // setup overlay
		peer.Overlay.DB = peer.DB.OverlayCache()
	}

	{ // setup garbage collection bloom filters
		log := peer.Log.Named("garbage-collection-bf")
		peer.GarbageCollection.Config = config.GarbageCollectionBF
		if config.GarbageCollectionBF.UseRangedLoop {
			log.Info("using ranged loop")

			var observer rangedloop.Observer
			if config.GarbageCollectionBF.UseSyncObserver {
				observer = bloomfilter.NewSyncObserver(log.Named("gc-bf"),
					config.GarbageCollectionBF,
					peer.Overlay.DB,
				)
			} else {
				observer = bloomfilter.NewObserver(log.Named("gc-bf"),
					config.GarbageCollectionBF,
					peer.Overlay.DB,
				)
			}

			provider := rangedloop.NewMetabaseRangeSplitter(metabaseDB, config.RangedLoop.AsOfSystemInterval, config.RangedLoop.BatchSize)
			peer.RangedLoop.Service = rangedloop.NewService(log.Named("rangedloop"), config.RangedLoop, provider, []rangedloop.Observer{observer})

			if !config.GarbageCollectionBF.RunOnce {
				peer.Services.Add(lifecycle.Item{
					Name:  "garbage-collection-bf",
					Run:   peer.RangedLoop.Service.Run,
					Close: peer.RangedLoop.Service.Close,
				})
				peer.Debug.Server.Panel.Add(
					debug.Cycle("Garbage Collection Bloom Filters", peer.RangedLoop.Service.Loop))
			}
		} else {
			log.Info("using segments loop")

			{ // setup metainfo
				peer.Metainfo.SegmentLoop = segmentloop.New(
					log.Named("segmentloop"),
					config.Metainfo.SegmentLoop,
					metabaseDB,
				)
				peer.Services.Add(lifecycle.Item{
					Name:  "metainfo:segmentloop",
					Run:   peer.Metainfo.SegmentLoop.Run,
					Close: peer.Metainfo.SegmentLoop.Close,
				})
			}

			peer.GarbageCollection.Service = bloomfilter.NewService(
				log,
				config.GarbageCollectionBF,
				peer.Overlay.DB,
				peer.Metainfo.SegmentLoop,
			)

			if !config.GarbageCollectionBF.RunOnce {
				peer.Services.Add(lifecycle.Item{
					Name: "garbage-collection-bf",
					Run:  peer.GarbageCollection.Service.Run,
				})
				peer.Debug.Server.Panel.Add(
					debug.Cycle("Garbage Collection Bloom Filters", peer.GarbageCollection.Service.Loop))
			}
		}
	}

	return peer, nil
}

// Run runs satellite garbage collection until it's either closed or it errors.
func (peer *GarbageCollectionBF) Run(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, ctx := errgroup.WithContext(ctx)

	pprof.Do(ctx, pprof.Labels("subsystem", "gc-bloomfilter"), func(ctx context.Context) {
		peer.Servers.Run(ctx, group)
		peer.Services.Run(ctx, group)

		if peer.GarbageCollection.Config.RunOnce {
			group.Go(func() error {
				if peer.GarbageCollection.Config.UseRangedLoop {
					_, err = peer.RangedLoop.Service.RunOnce(ctx)
				} else {
					err = peer.GarbageCollection.Service.RunOnce(ctx)
				}
				cancel()
				return err
			})
		}

		pprof.Do(ctx, pprof.Labels("name", "subsystem-wait"), func(ctx context.Context) {
			err = group.Wait()
		})
	})

	return err
}

// Close closes all the resources.
func (peer *GarbageCollectionBF) Close() error {
	return errs.Combine(
		peer.Servers.Close(),
		peer.Services.Close(),
	)
}
