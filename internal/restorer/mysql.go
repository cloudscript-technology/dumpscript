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

// MySQL restores using `mysql` (with `mariadb` fallback).
type MySQL struct {
	cfg *config.Config
	log *slog.Logger
}

func init() {
	Register(config.DBMySQL, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewMySQL(cfg, log)
	})
}

func NewMySQL(cfg *config.Config, log *slog.Logger) *MySQL {
	return &MySQL{cfg: cfg, log: log}
}

func (m *MySQL) Restore(ctx context.Context, gzPath string) error {
	clientCmd, err := resolveMySQLClient()
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

	m.log.Info("executing mysql restore", "cmd", clientCmd, "args", args, "src", gzPath)
	cmd := exec.CommandContext(ctx, clientCmd, args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+m.cfg.DB.Password)
	return streamGzipToStdin(cmd, gzPath)
}

func (m *MySQL) createDatabase(ctx context.Context, clientCmd string) error {
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

func resolveMySQLClient() (string, error) {
	if _, err := exec.LookPath("mysql"); err == nil {
		return "mysql", nil
	}
	if _, err := exec.LookPath("mariadb"); err == nil {
		return "mariadb", nil
	}
	return "", fmt.Errorf("no mysql or mariadb client found on PATH")
}
