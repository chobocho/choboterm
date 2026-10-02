package main

import (
	"io"
	"os"
	"path"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpFS opens an SFTP channel on an existing SSH connection,
// so no second login is needed.
type sftpFS struct {
	c *sftp.Client
}

func newSFTP(client *ssh.Client) (*sftpFS, error) {
	c, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
	if err != nil {
		return nil, err
	}
	return &sftpFS{c: c}, nil
}

func (s *sftpFS) Getwd() (string, error) { return s.c.Getwd() }

func (s *sftpFS) List(dir string) ([]FileEntry, error) {
	infos, err := s.c.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	list := make([]FileEntry, 0, len(infos))
	for _, fi := range infos {
		e := FileEntry{
			Name:    fi.Name(),
			Size:    fi.Size(),
			IsDir:   fi.IsDir(),
			ModTime: fi.ModTime().Format("2006-01-02 15:04"),
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, err := s.c.Stat(path.Join(dir, fi.Name())); err == nil {
				e.IsDir = target.IsDir()
				e.Size = target.Size()
			}
		}
		list = append(list, e)
	}
	return list, nil
}

func (s *sftpFS) Download(remotePath string, w io.Writer) error {
	f, err := s.c.Open(remotePath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteTo(w)
	return err
}

func (s *sftpFS) Upload(remotePath string, r io.Reader) error {
	f, err := s.c.Create(remotePath)
	if err != nil {
		return err
	}
	if _, err = f.ReadFrom(r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (s *sftpFS) Close() error { return s.c.Close() }
