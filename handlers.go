package main

import (
	"fmt"
	"net/http"
	"strconv"
)

const (
	loggedInUserKey = "logged_in_user_email"
)

func (app *application) readIntWithDefault(r *http.Request, key string, def int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return v
}

func (app *application) home(w http.ResponseWriter, r *http.Request) {

	filter := Filter{
		Query:    r.URL.Query().Get("q"),
		OrderBy:  r.URL.Query().Get("order_by"),
		Page:     app.readIntWithDefault(r, "page", 1),
		PageSize: app.readIntWithDefault(r, "page_size", 50),
	}

	posts, metadata, err := app.postRepo.GetAll(filter)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	app.infoLog.Printf("\nMetadata: %+v\n", metadata)

	app.render(w, r, "index.html", &templateData{
		Posts:    posts,
		Metadata: metadata,
		NextLink: fmt.Sprintf("/?q=%s&order_by=%s&page=%d&page_size=%d",
			filter.Query, filter.OrderBy, metadata.NextPage, filter.PageSize),
		PrevLink: fmt.Sprintf("/?q=%s&order_by=%s&page=%d&page_size=%d",
			filter.Query, filter.OrderBy, metadata.PrevPage, filter.PageSize),
	})
}

func (app *application) login(w http.ResponseWriter, r *http.Request) {
	if app.isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		form := NewForm(r.PostForm)
		form.Required("email", "password").
			MaxLength("email", 255).
			MaxLength("password", 255).
			MinLength("password", 3).
			MinLength("email", 3).
			IsEmail("email")

		if !form.Valid() {
			form.Errors.Add("generic", "The data submitted was not valid")
			app.render(w, r, "login.html", &templateData{
				Form: form,
			})
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")
		_, err := app.userRepo.Authenticate(email, password)
		if err != nil {
			form.Errors.Add("generic", err.Error())
			app.render(w, r, "login.html", &templateData{
				Form: form,
			})
			return
		}

		app.session.Put(r, loggedInUserKey, email)
		app.session.Put(r, "flash", "You're logged in!")

		app.infoLog.Println("Logged in ")

		http.Redirect(w, r, "/submit", http.StatusSeeOther)
		return
	}

	app.render(w, r, "login.html", &templateData{
		Form: NewForm(r.PostForm),
	})
}

func (app *application) register(w http.ResponseWriter, r *http.Request) {
	if app.isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		form := NewForm(r.PostForm)
		form.Required("email", "password", "name").
			MaxLength("email", 255).
			MaxLength("password", 255).
			MinLength("password", 3).
			MinLength("email", 3).
			MinLength("name", 3).
			IsEmail("email")

		if !form.Valid() {
			form.Errors.Add("generic", "The data submitted was not valid")
			app.render(w, r, "register.html", &templateData{
				Form: form,
			})
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")
		name := r.FormValue("name")
		avatar := r.FormValue("avatar")

		_, err := app.userRepo.CreateUser(name, email, password, avatar)
		if err != nil {
			form.Errors.Add("generic", err.Error())
			app.render(w, r, "register.html", &templateData{
				Form: form,
			})
			return
		}

		app.session.Put(r, "flash", "You've been registered successfully!")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	app.render(w, r, "register.html", &templateData{
		Form: NewForm(r.PostForm),
	})
}

func (app *application) about(w http.ResponseWriter, r *http.Request) {
	app.render(w, r, "about.html", nil)
}

func (app *application) contact(w http.ResponseWriter, r *http.Request) {
	app.render(w, r, "contact.html", nil)
}

func (app *application) submit(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		form := NewForm(r.PostForm)
		form.Required("title", "url").
			MaxLength("title", 255).
			MaxLength("url", 255).
			MinLength("url", 3)

		if !form.Valid() {
			form.Errors.Add("generic", "The data submitted was not valid")
			app.render(w, r, "submit.html", &templateData{
				Form: form,
			})
			return
		}

		title := r.FormValue("title")
		url := r.FormValue("url")

		user := app.getUserFromContext(r.Context())
		id, err := app.postRepo.CreatePost(title, url, user.ID)
		if err != nil {
			app.errorLog.Printf("error creating post: %s\n", err.Error())
			form.Errors.Add("generic", "Post creation failed")
			app.render(w, r, "submit.html", &templateData{
				Form: form,
			})
			return
		}

		app.session.Put(r, "flash", "Post submitted successfully!")
		app.infoLog.Printf("Post created with id: %d", id)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	app.render(w, r, "submit.html", &templateData{
		Form: NewForm(r.PostForm),
	})
}

func (app *application) vote(w http.ResponseWriter, r *http.Request) {
	postID := app.readIntWithDefault(r, "post_id", 0)
	u := app.getUserFromContext(r.Context())

	err := app.postRepo.AddVote(u.ID, postID)
	if err != nil {
		app.errorLog.Printf("error adding vote: %s\n", err.Error())
		app.session.Put(r, "flash", "voting failed!")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	app.session.Put(r, "flash", fmt.Sprintf("You voted for post with id #%d", postID))
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return
}

func (app *application) comments(w http.ResponseWriter, r *http.Request) {
	postID := app.readIntWithDefault(r, "post_id", 0)
	u := app.getUserFromContext(r.Context())
	post, err := app.postRepo.GetByID(postID)
	if err != nil {
		app.errorLog.Printf("error getting post with id #%d: %s\n", postID, err.Error())
		app.session.Put(r, "flash", "post not found!")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	comments, err := app.postRepo.GetComments(postID)
	if err != nil {
		app.errorLog.Printf("error getting comments: %s\n", err.Error())
		app.session.Put(r, "flash", "error getting comments")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		form := NewForm(r.PostForm)
		form.Required("comment").
			MinLength("comment", 5).
			MaxLength("comment", 160)
		if !form.Valid() {
			form.Errors.Add("generic", "The data submitted was not valid")
			app.render(w, r, "comment.html", &templateData{
				Form:     form,
				Comments: comments,
				Post:     post,
			})
			return
		}

		_, err := app.postRepo.AddComment(u.ID, postID, r.FormValue("comment"))
		if err != nil {
			app.errorLog.Printf("error adding comment: %s\n", err.Error())
			app.session.Put(r, "flash", "error adding comment!")
			http.Redirect(w, r, fmt.Sprintf("/comments?post_id=%d", postID), http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/comments?post_id=%d", postID), http.StatusSeeOther)
		return
	}

	app.render(w, r, "comments.html", &templateData{
		Form:     NewForm(r.PostForm),
		Comments: comments,
		Post:     post,
	})
}

func (app *application) logout(w http.ResponseWriter, r *http.Request) {
	app.session.Remove(r, loggedInUserKey)
	app.session.Put(r, "flash", "You're logged out!")

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
