package cache

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/telepresenceio/telepresence/v2/pkg/filelocation"
)

type DaemonInfo struct {
	Options  map[string]string
	InDocker bool
}

const (
	daemonsDirName    = "daemons"
	keepAliveInterval = 5 * time.Second
)

func SaveDaemonInfo(ctx context.Context, object *DaemonInfo, file string) error {
	return SaveToUserCache(ctx, object, filepath.Join(daemonsDirName, file))
}

func DeleteDaemonInfo(ctx context.Context, file string) error {
	return DeleteFromUserCache(ctx, filepath.Join(daemonsDirName, file))
}

func DaemonInfoExists(ctx context.Context, file string) (bool, error) {
	return ExistsInCache(ctx, filepath.Join(daemonsDirName, file))
}

func WatchDaemonInfos(ctx context.Context, onChange func(context.Context) error, files ...string) error {
	return WatchUserCache(ctx, daemonsDirName, onChange, files...)
}

func LoadDaemonInfos(ctx context.Context) ([]*DaemonInfo, error) {
	files, err := daemonInfoFiles(ctx)
	if err != nil {
		return nil, err
	}

	DaemonInfos := make([]*DaemonInfo, len(files))
	for i, file := range files {
		if err = LoadFromUserCache(ctx, &DaemonInfos[i], filepath.Join(daemonsDirName, file.Name())); err != nil {
			return nil, err
		}
	}
	return DaemonInfos, nil
}

func daemonInfoFiles(ctx context.Context) ([]fs.DirEntry, error) {
	dir, err := filelocation.AppUserCacheDir(ctx)
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(filepath.Join(dir, daemonsDirName))
	if err != nil {
		if os.IsNotExist(err) {
			err = nil
		}
		return nil, err
	}
	active := make([]fs.DirEntry, 0, len(files))
	for _, file := range files {
		fi, err := file.Info()
		if err != nil {
			return nil, err
		}
		if fi.ModTime().Add(keepAliveInterval + 200*time.Millisecond).Before(time.Now()) {
			// File has gone stale
			if err = DeleteFromUserCache(ctx, filepath.Join(daemonsDirName, file.Name())); err != nil {
				return nil, err
			}
		} else {
			active = append(active, file)
		}
	}
	return active, err
}

var diNameRx = regexp.MustCompile(`^(.+?)-(\d+)\.json$`)

func DaemonPortForName(ctx context.Context, context string) (int, error) {
	files, err := daemonInfoFiles(ctx)
	if err != nil {
		return 0, err
	}
	for _, file := range files {
		if m := diNameRx.FindStringSubmatch(file.Name()); m != nil && m[1] == context {
			port, _ := strconv.Atoi(m[2])
			return port, nil
		}
	}
	return 0, os.ErrNotExist
}

func DaemonInfoFile(name string, port int) string {
	return fmt.Sprintf("%s-%d.json", name, port)
}

// KeepDaemonInfoAlive updates the access and modification times of the given DaemonInfo
// periodically so that it never gets older than keepAliveInterval. This means that
// any file with a modification time older than the current time minus two keepAliveIntervals
// can be considered stale and should be removed.
//
// The alive poll ends and the DaemonInfo is deleted when the context is cancelled.
func KeepDaemonInfoAlive(ctx context.Context, file string) error {
	dir, err := filelocation.AppUserCacheDir(ctx)
	if err != nil {
		return err
	}
	daemonFile := filepath.Join(dir, daemonsDirName, file)
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = DeleteDaemonInfo(ctx, file)
			return nil
		case now := <-ticker.C:
			if err := os.Chtimes(daemonFile, now, now); err != nil {
				if os.IsNotExist(err) {
					// File is removed, so stop trying to update its timestamps
					return nil
				}
				return fmt.Errorf("failed to update timestamp on %s: %w", daemonFile, err)
			}
		}
	}
}
