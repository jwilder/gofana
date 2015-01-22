package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type Dashboard struct {
	Id    string   `json:"id"`
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
	Path  string   `json:"-"`
}

type DashboardRepository struct {
	Dir        string
	dashboards []*Dashboard
}

func (d *Dashboard) String() string {
	return fmt.Sprintf("id=%s title=%s tags=%s path=%s", d.Id, d.Title, d.Tags, d.Path)
}

func (d *DashboardRepository) unmarshalDashboard(data io.Reader) (*Dashboard, error) {
	body, err := ioutil.ReadAll(data)
	if err != nil {
		return nil, err
	}

	var dash Dashboard
	err = json.Unmarshal(body, &dash)
	if err != nil {
		return nil, err
	}
	return &dash, nil
}

func (d *DashboardRepository) Load() error {
	d.dashboards = []*Dashboard{}

	err := filepath.Walk(d.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		f, err := os.OpenFile(path, os.O_RDONLY, 0666)
		if err != nil {
			return err
		}
		defer f.Close()

		dash, err := d.unmarshalDashboard(f)
		if err != nil {
			return err
		}

		dash.Path = path
		d.dashboards = append(d.dashboards, dash)
		return nil
	})
	if err != nil {
		return err
	}
	log.Printf("Loaded %d dashboards\n", len(d.dashboards))
	return nil
}

func (d *DashboardRepository) Search(query string) ([]*Dashboard, error) {
	return d.dashboards, nil
}

func (d *DashboardRepository) Exists(id string) bool {
	path := filepath.Join(d.Dir, id+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

func (d *DashboardRepository) Delete(id string) error {
	dashboards := []*Dashboard{}

	for _, d := range d.dashboards {
		if d.Id == id {
			err := os.Remove(d.Path)
			if err != nil {
				return err
			}
			log.Printf("Deleted dashboard %s", id)
		} else {
			dashboards = append(dashboards, d)
		}
	}
	d.dashboards = dashboards
	return nil
}

func (d *DashboardRepository) Get(id string) ([]byte, error) {
	path := filepath.Join(d.Dir, id+".json")

	f, err := os.OpenFile(path, os.O_RDONLY, 0666)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ioutil.ReadAll(f)
}

func (d *DashboardRepository) Save(id string, data []byte) error {
	path := filepath.Join(d.Dir, id+".json.tmp")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	n, err := f.Write(data)
	if err != nil {
		return err
	}

	if n != len(data) {
		return fmt.Errorf("wrote %d, expected %d", n, len(data))
	}

	err = f.Sync()
	if err != nil {
		return err
	}

	newPath := filepath.Join(d.Dir, id+".json")
	err = os.Rename(path, newPath)
	if err != nil {
		return err
	}

	dash, err := d.unmarshalDashboard(bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	dash.Path = newPath
	d.update(dash)
	log.Printf("Saved dashboard %s", id)

	return nil
}

func (d *DashboardRepository) update(dash *Dashboard) {
	for _, d := range d.dashboards {
		if d.Id == dash.Id {
			d.Title = dash.Title
			d.Tags = dash.Tags
			return
		}
	}
	d.dashboards = append(d.dashboards, dash)
}
