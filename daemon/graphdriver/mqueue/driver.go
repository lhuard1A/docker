package mqueue

import (
	"fmt"
	"os"
	"path"
	"sync"
	"syscall"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/daemon/graphdriver"
)

func init() {
	graphdriver.Register("mqueue", Init)
}

func Init(home string, options []string) (graphdriver.Driver, error) {
	d := &Driver{
		home:   home,
		active: make(map[string]*ActiveMount),
	}
	return graphdriver.NaiveDiffDriver(d), nil
}

type ActiveMount struct {
	count   int
	path    string
	mounted bool
}
type Driver struct {
	home       string
	sync.Mutex // Protects concurrent modification to active
	active     map[string]*ActiveMount
}

func (d *Driver) String() string {
	return "mqueue"
}

func (d *Driver) Status() [][2]string {
	return nil
}

func (d *Driver) Cleanup() error {
	return nil
}

func (d *Driver) Create(id, parent string) error {
	// mqueue cannot have parent
	if parent != "" {
		return fmt.Errorf("mqueue driver doesn't support parent")
	}

	dir := d.dir(id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return nil
}

func (d *Driver) dir(id string) string {
	return path.Join(d.home, "dir", path.Base(id))
}

func (d *Driver) Remove(id string) error {
	if _, err := os.Stat(d.dir(id)); err != nil {
		return err
	}
	return os.RemoveAll(d.dir(id))
}

func (d *Driver) Get(id, mountLabel string) (string, error) {
	d.Lock()
	defer d.Unlock()

	mount := d.active[id]
	if mount != nil {
		mount.count++
		return mount.path, nil
	} else {
		mount = &ActiveMount{count: 1}
	}

	dir := d.dir(id)
	if _, err := os.Stat(dir); err != nil {
		return "", err
	}

	if err := syscall.Mount("mqueue", dir, "mqueue", syscall.MS_NODEV|syscall.MS_NOSUID|syscall.MS_NOEXEC, ""); err != nil {
		return "", fmt.Errorf("error creating mqueue mount to %s: %v", dir, err)
	}

	mount.path = dir
	mount.mounted = true
	d.active[id] = mount

	return mount.path, nil
}

func (d *Driver) Put(id string) error {
	d.Lock()
	defer d.Unlock()

	mount := d.active[id]
	if mount == nil {
		log.Debugf("Put on a non-mounted device %s", id)
		return nil
	}

	mount.count--
	if mount.count > 0 {
		return nil
	}

	defer delete(d.active, id)
	if mount.mounted {
		err := syscall.Unmount(mount.path, 0)
		if err != nil {
			log.Debugf("Failed to unmount %s mqueue: %v", id, err)
		}
		return err
	}
	return nil
}

func (d *Driver) Exists(id string) bool {
	_, err := os.Stat(d.dir(id))
	return err == nil
}
