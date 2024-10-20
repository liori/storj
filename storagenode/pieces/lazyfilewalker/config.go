// Copyright (C) 2023 Storj Labs, Inc.
// See LICENSE for copying information.

package lazyfilewalker

import (
	"strconv"

	"storj.io/storj/storagenode/blobstore/filestore"
)

// Config is the config for lazyfilewalker process.
type Config struct {
	// TODO: just trying to match the names in storagenodedb.Config. Change these to be more descriptive.
	Storage   string `help:"path to the storage database directory"`
	Info      string `help:"path to the piecestore db"`
	Info2     string `help:"path to the info database"`
	Driver    string `help:"database driver to use" default:"sqlite3"`
	Pieces    string `help:"path to store pieces in"`
	Filestore filestore.Config

	LowerIOPriority bool `help:"if true, the process will run with lower IO priority" default:"true"`
}

// Args returns the flags to be passed lazyfilewalker process.
func (config *Config) Args() []string {
	// TODO: of course, we shouldn't hardcode this.
	return []string{
		"--storage", config.Storage,
		"--info", config.Info,
		"--info2", config.Info2,
		"--pieces", config.Pieces,
		"--driver", config.Driver,
		"--filestore.write-buffer-size", config.Filestore.WriteBufferSize.String(),
		// set log output to stderr, so it doesn't interfere with the output of the command
		"--log.output", "stderr",
		// use the json formatter in the subprocess, so we could read lines and re-log them in the main process
		// with all the fields intact.
		"--log.encoding", "json",
		"--lower-io-priority", strconv.FormatBool(config.LowerIOPriority),
	}
}
