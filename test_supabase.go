package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// 从环境变量或默认值读取配置
	host := os.Getenv("DB_HOST")
	if host == "" {
		// 使用项目特定的连接池地址
		host = "uvbojcqbfobmqrjturza.pooler.supabase.com"
	}

	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "6543"
	}

	user := os.Getenv("DB_USER")
	if user == "" {
		user = "postgres"
	}

	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		password = "qinghuaUNIVER123"
	}

	database := os.Getenv("DB_NAME")
	if database == "" {
		database = "postgres"
	}

	// 解析主机名
	ips, err := net.LookupIP(host)
	if err != nil {
		log.Printf("⚠️ 无法解析主机: %v", err)
	} else {
		var ipv4 string
		for _, ip := range ips {
			if ip.To4() != nil {
				ipv4 = ip.String()
				break
			}
		}
		if ipv4 != "" {
			log.Printf("📌 解析到 IPv4: %s", ipv4)
		}
	}

	// 构建连接字符串（添加 tenant identifier）
	// project ID: uvbojcqbfobmqrjturza
	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=require&sni_hostname=%s&options=-c%20external_id=uvbojcqbfobmqrjturza",
		user, password, host, port, database, host,
	)

	log.Printf("尝试连接到: %s:%s/%s (使用 SNI: %s)", host, port, database, host)

	// 创建 TLS 配置（禁用证书验证，仅用于测试）
	tlsConfig := &tls.Config{
		ServerName:         host, // 设置 SNI
		InsecureSkipVerify: true, // 禁用证书验证（仅测试使用）
	}

	// 创建连接配置
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		log.Fatalf("❌ 无法解析连接字符串: %v", err)
	}

	// 设置 TLS 配置
	config.ConnConfig.TLSConfig = tlsConfig
	config.MaxConns = 1
	config.MinConns = 0

	// 连接
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		log.Fatalf("❌ 无法创建连接池: %v", err)
	}
	defer pool.Close()

	// 测试连接
	err = pool.Ping(ctx)
	if err != nil {
		log.Fatalf("❌ 无法连接到数据库: %v", err)
	}

	log.Println("✅ 成功连接到 Supabase PostgreSQL 数据库！")

	// 查询数据库版本
	var version string
	err = pool.QueryRow(ctx, "SELECT version();").Scan(&version)
	if err != nil {
		log.Fatalf("❌ 无法查询数据库版本: %v", err)
	}

	log.Printf("📊 数据库版本: %s", version)
}
