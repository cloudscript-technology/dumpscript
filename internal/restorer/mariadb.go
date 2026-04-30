package restorer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// MariaDB restores using `mariadb` (with `mysql` fallback).
type MariaDB struct {
	cfg *config.Config
	log *slog.Logger
}

func init() {
	Register(config.DBMariaDB, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewMariaDB(cfg, log)
	})
}

func NewMariaDB(cfg *config.Config, log *slog.Logger) *MariaDB {
	return &MariaDB{cfg: cfg, log: log}
}

func (m *MariaDB) Restore(ctx context.Context, gzPath string) error {
	clientCmd, err := resolveMariaDBClient()
	if err != nil {
		return err
	}

	if m.cfg.DB.CreateDB && m.cfg.DB.Name != "" {
		if err := m.createDatabase(ctx, clientCmd); err != nil {
			m.log.Warn("create database failed (may already exist)", "err", err)
		}
	}

	args := []string{
		"-h", m.cfg.DB.Host,
		"-P", strconv.Itoa(m.cfg.DB.Port),
		"-u", m.cfg.DB.User,
	}
	if m.cfg.DB.Name != "" {
		args = append(args, m.cfg.DB.Name)
	}

	m.log.Info("executing mariadb restore", "cmd", clientCmd, "args", args, "src", gzPath)
	cmd := exec.CommandContext(ctx, clientCmd, args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+m.cfg.DB.Password)
	return streamGzipToStdin(cmd, gzPath)
}

func (m *MariaDB) createDatabase(ctx context.Context, clientCmd string) error {
	cmd := exec.CommandContext(ctx, clientCmd,
		"-h", m.cfg.DB.Host,
		"-P", strconv.Itoa(m.cfg.DB.Port),
		"-u", m.cfg.DB.User,
		"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", m.cfg.DB.Name),
	)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+m.cfg.DB.Password)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func resolveMariaDBClient() (string, error) {
	if _, err := exec.LookPath("mariadb"); err == nil {
		return "mariadb", nil
	}
	if _, err := exec.LookPath("mysql"); err == nil {
		return "mysql", nil
	}
	return "", fmt.Errorf("no mariadb or mysql client found on PATH")
}
