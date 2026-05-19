// internal/repository/mysql_document_repository.go
package repository

import (
	"context"
	"database/sql"
	"fmt"

	appmodel "stock_rag/internal/model"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLDocumentRepository 实现了 DocumentRepository 接口，用于 MySQL 数据库。
type MySQLDocumentRepository struct {
	db *sql.DB
}

func NewMySQLDocumentRepository(host, port, user, password, dbname string) (*MySQLDocumentRepository, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, password, host, port, dbname)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		return nil, err
	}

	// 创建表（如果不存在）
	if err := CreateTable(db); err != nil {
		return nil, err
	}

	return &MySQLDocumentRepository{db: db}, nil
}

// CreateTable 创建文档表
func CreateTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS documents (
			id INT AUTO_INCREMENT PRIMARY KEY,
			title VARCHAR(255) NOT NULL,
			content TEXT NOT NULL,
			stock_code VARCHAR(20),
			company_name VARCHAR(100),
			doc_type VARCHAR(50),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`
	_, err := db.Exec(query)
	return err
}

// ListDocuments 从 MySQL 数据库获取文档列表
func (r *MySQLDocumentRepository) ListDocuments(ctx context.Context) ([]appmodel.Document, error) {
	query := `
		SELECT id, title, content, stock_code, company_name, doc_type, created_at, updated_at 
		FROM documents 
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]appmodel.Document, 0)
	for rows.Next() {
		var doc appmodel.Document
		var id int
		var createdAt, updatedAt string

		err := rows.Scan(&id, &doc.Title, &doc.Content, &doc.StockCode, &doc.CompanyName, &doc.DocType, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}

		docs = append(docs, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return docs, nil
}
