package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/dromara/carbon/v2"
)

var (
	ErrDuplicatePostTitle = errors.New("duplicate post title")
	ErrDuplicateVote      = errors.New("duplicate vote")
)

type Post struct {
	ID           int       `json:"id"`
	Title        string    `json:"title"`
	URL          string    `json:"url"`
	UserID       int       `json:"user_id"`
	UserName     string    `json:"user_name"`
	CreatedAt    time.Time `json:"created_at"`
	CommentCount int       `json:"comment_count"`
	VoteCount    int       `json:"vote_count"`
	TotalRecords int       `json:"total_records"`
}

type Comment struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	UserID    int       `json:"user_id"`
	PostID    int       `json:"post_id"`
	UserName  string    `json:"user_name"`
	CreatedAt time.Time `json:"created_at"`
}

type Filter struct {
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	OrderBy  string `json:"order_by"`
	Query    string `json:"query"`
}

func (f *Filter) Validate() error {
	if f.PageSize <= 0 || f.PageSize > 100 {
		return errors.New("invalid page size: 1 to 100")
	}
	return nil
}

type Metadata struct {
	CurrentPage  int `json:"current_page"`
	PageSize     int `json:"page_size"`
	FirstPage    int `json:"first_page"`
	NextPage     int `json:"next_page"`
	PrevPage     int `json:"prev_page"`
	LastPage     int `json:"last_page"`
	TotalRecords int `json:"total_records"`
}

func calculateMetadata(totalRecords, page, pageSize int) Metadata {
	if totalRecords == 0 {
		return Metadata{}
	}
	meta := Metadata{
		CurrentPage:  page,
		PageSize:     pageSize,
		FirstPage:    1,
		LastPage:     int(math.Ceil(float64(totalRecords) / float64(pageSize))),
		TotalRecords: totalRecords,
	}
	if meta.PrevPage = page - 1; meta.PrevPage < meta.FirstPage {
		meta.PrevPage = 0
	}
	if meta.NextPage = page + 1; meta.NextPage > meta.LastPage {
		meta.NextPage = 0
	}
	return meta
}

type PostRepository interface {
	CreatePost(title, url string, userID int) (int, error)
	AddComment(userID, postID int, body string) (int, error)
	AddVote(userID, postID int) error
	GetAll(filter Filter) ([]Post, Metadata, error)
	GetByID(id int) (*Post, error)
	GetComments(postID int) ([]Comment, error)
}

type SQLPostRepository struct {
	db *sql.DB
}

// NewSQLPostRepository creates a new PostRepository
func NewSQLPostRepository(db *sql.DB) PostRepository {
	return &SQLPostRepository{db: db}
}

func (r *SQLPostRepository) CreatePost(title, url string, userID int) (int, error) {
	stmt := "INSERT INTO posts (title, url, user_id) VALUES (?, ?, ?)"

	res, err := r.db.Exec(stmt, title, url, userID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: posts.title") {
			return 0, ErrDuplicatePostTitle
		}
		return 0, err
	}

	postID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(postID), nil
}

func (r *SQLPostRepository) AddComment(userID, postID int, body string) (int, error) {
	stmt := "INSERT INTO comments (user_id, post_id, body) VALUES (?, ?, ?)"
	res, err := r.db.Exec(stmt, userID, postID, body)
	if err != nil {
		return 0, err
	}

	commentID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(commentID), nil
}

func (r *SQLPostRepository) AddVote(userID, postID int) error {
	stmt := "INSERT INTO votes (user_id, post_id) VALUES (?, ?)"

	_, err := r.db.Exec(stmt, userID, postID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") ||
			strings.Contains(err.Error(), "PRIMARY KEY constraint failed") {
			return ErrDuplicateVote
		}
		return err
	}
	return nil
}

func (r *SQLPostRepository) GetByID(id int) (*Post, error) {

	query := `
	SELECT p.id, p.title, p.url, p.user_id, p.created_at,
	u.name as user_name, 
	COUNT(DISTINCT c.id) as comment_count,
	COUNT(DISTINCT v.user_id) as vote_count
	FROM posts p
	LEFT JOIN users u ON p.user_id = u.id
	LEFT JOIN comments c ON p.id = c.post_id
	LEFT JOIN votes v ON p.id = v.post_id
	WHERE p.id = ?
	GROUP BY p.id, p.title, p.url, p.user_id, p.created_at, u.name
`
	row := r.db.QueryRow(query, id)
	var post Post
	err := row.Scan(&post.ID,
		&post.Title,
		&post.URL,
		&post.UserID,
		&post.CreatedAt,
		&post.UserName,
		&post.CommentCount,
		&post.VoteCount)

	if err != nil {
		return nil, err
	}

	return &post, nil
}

func (r *SQLPostRepository) GetAll(filter Filter) ([]Post, Metadata, error) {
	if err := filter.Validate(); err != nil {
		return nil, Metadata{}, err
	}

	baseQuery := `
		SELECT
			COUNT(*) OVER() as total_records,
			p.id, p.title, p.url, p.user_id, p.created_at,
			u.name as user_name,
			COUNT(DISTINCT c.id) as comment_count,
			COUNT(DISTINCT v.user_id) as vote_count
		FROM posts p
		LEFT JOIN users u ON p.user_id = u.id
		LEFT JOIN comments c ON p.id = c.post_id
		LEFT JOIN votes v ON p.id = v.post_id
	`

	var args []interface{}
	argIndex := 1

	if filter.Query != "" {
		baseQuery += " WHERE LOWER(p.title) LIKE ?"
		args = append(args, "%"+strings.ToLower(filter.Query)+"%")
		argIndex++
	}

	baseQuery += " GROUP BY p.id, p.title, p.url, p.user_id, p.created_at, u.name"
	if filter.OrderBy == "popular" {
		baseQuery += " ORDER BY vote_count DESC, p.created_at DESC"
	} else {
		baseQuery += " ORDER BY p.created_at DESC"
	}

	limit := filter.PageSize
	offset := (filter.Page - 1) * filter.PageSize
	baseQuery += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := r.db.Query(baseQuery, args...)
	if err != nil {
		return nil, Metadata{}, err
	}
	defer rows.Close()

	var posts []Post
	var totalRecords int
	for rows.Next() {
		var post Post
		err = rows.Scan(&totalRecords, &post.ID, &post.Title, &post.URL, &post.UserID,
			&post.CreatedAt, &post.UserName, &post.CommentCount, &post.VoteCount)
		if err != nil {
			return nil, Metadata{}, err
		}
		post.TotalRecords = totalRecords
		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		return nil, Metadata{}, err
	}

	if len(posts) == 0 {
		return []Post{}, Metadata{}, err
	}

	metadata := calculateMetadata(totalRecords, filter.Page, filter.PageSize)
	return posts, metadata, err
}

func (r *SQLPostRepository) GetComments(postID int) ([]Comment, error) {
	query := `
		SELECT c.id, c.body, c.user_id, c.post_id, c.created_at, u.name as user_name
		FROM comments c
		LEFT JOIN users u ON c.user_id = u.id
		WHERE c.post_id = ?
		ORDER BY c.created_at ASC
	`

	rows, err := r.db.Query(query, postID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var comment Comment
		err = rows.Scan(&comment.ID, &comment.Body, &comment.UserID, &comment.PostID,
			&comment.CreatedAt, &comment.UserName)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(comments) == 0 {
		return []Comment{}, nil
	}
	return comments, nil
}

func (p *Post) GetVoteCountsHuman() string {
	if p.VoteCount > 1 {
		return fmt.Sprintf("%d votes", p.VoteCount)
	}

	return fmt.Sprintf("%d vote", p.VoteCount)
}

func (p *Post) GetCommentCountsHuman() string {
	if p.CommentCount > 1 {
		return fmt.Sprintf("%d comments", p.CommentCount)
	}

	return fmt.Sprintf("%d comment", p.CommentCount)
}

func (p *Post) CreatedAtHuman() string {
	return carbon.NewCarbon(p.CreatedAt).DiffForHumans()
}

func (p *Post) Host() string {
	ur, err := url.Parse(p.URL)
	if err != nil {
		return "<invalid-host>"
	}
	return ur.Hostname()
}
