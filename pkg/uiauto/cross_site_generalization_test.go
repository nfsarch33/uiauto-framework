package uiauto

import (
	"context"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/domheal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func moodleMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Moodle", PageID: "moodle_quiz", PatternID: "quiz_form",
			OrigHTML:    `<div id="page-content"><form id="responseform"><div class="que multichoice"><div class="qtext">What is 2+2?</div><div class="answer"><input type="radio" name="q1" value="3"><label>3</label><input type="radio" name="q1" value="4"><label>4</label></div></div></form></div>`,
			MutatedHTML: `<div id="page-content"><form id="responseform"><div class="question-container" data-qtype="multichoice"><div class="question-text">What is 2+2?</div><div class="answer-options"><label class="option"><input type="radio" name="q1" value="3">3</label><label class="option"><input type="radio" name="q1" value="4">4</label></div></div></form></div>`,
			Selector:    "div.que.multichoice", Desc: "Moodle quiz question class restructure",
		},
		{
			Site: "Moodle", PageID: "moodle_grades", PatternID: "grade_report",
			OrigHTML:    `<table id="user-grades" class="generaltable"><thead><tr><th>Activity</th><th>Grade</th><th>Range</th></tr></thead><tbody><tr><td>Assignment 1</td><td>85</td><td>0-100</td></tr></tbody></table>`,
			MutatedHTML: `<div id="user-grades" class="grade-report-container"><div class="grade-header"><span>Activity</span><span>Grade</span><span>Range</span></div><div class="grade-row"><span>Assignment 1</span><span>85</span><span>0-100</span></div></div>`,
			Selector:    "table#user-grades", Desc: "Moodle grade table to div-based layout",
		},
		{
			Site: "Moodle", PageID: "moodle_forum", PatternID: "forum_post",
			OrigHTML:    `<div class="forumpost"><div class="row header"><div class="topic"><h3>Discussion Topic</h3></div><div class="author">Student A</div></div><div class="row content"><div class="posting">This is a discussion post</div></div></div>`,
			MutatedHTML: `<article class="forum-discussion" data-postid="123"><header><h3 class="discussion-title">Discussion Topic</h3><span class="post-author">Student A</span></header><div class="post-body">This is a discussion post</div></article>`,
			Selector:    "div.forumpost", Desc: "Moodle forum post from div to semantic article",
		},
		{
			Site: "Moodle", PageID: "moodle_nav", PatternID: "course_nav",
			OrigHTML:    `<div id="nav-drawer" class="drawer"><nav class="list-group"><a class="list-group-item" href="/mod/assign">Assignments</a><a class="list-group-item" href="/mod/quiz">Quizzes</a></nav></div>`,
			MutatedHTML: `<aside id="course-sidebar" role="navigation"><ul class="course-menu"><li><a href="/mod/assign" class="menu-link">Assignments</a></li><li><a href="/mod/quiz" class="menu-link">Quizzes</a></li></ul></aside>`,
			Selector:    "nav.list-group", Desc: "Moodle nav drawer to sidebar menu",
		},
		{
			Site: "Moodle", PageID: "moodle_submit", PatternID: "submit_assignment",
			OrigHTML:    `<div id="fitem_id_submitbutton"><input type="submit" id="id_submitbutton" value="Save changes" class="btn btn-primary"></div>`,
			MutatedHTML: `<div class="form-actions" data-region="footer"><button type="submit" class="btn btn-primary" data-action="save">Save changes</button></div>`,
			Selector:    "input#id_submitbutton", Desc: "Moodle submit from input to button element",
		},
		{
			Site: "Moodle", PageID: "moodle_calendar", PatternID: "calendar_events",
			OrigHTML:    `<div class="calendarwrapper"><table class="calendarmonth"><tbody><tr><td class="day hasevent"><a href="/calendar/view.php">15</a><div class="eventlist">Assignment Due</div></td></tr></tbody></table></div>`,
			MutatedHTML: `<div class="calendar-container" data-view="month"><div class="calendar-grid"><div class="calendar-day has-events" data-date="2026-03-15"><span class="day-number">15</span><div class="event-badge">Assignment Due</div></div></div></div>`,
			Selector:    "table.calendarmonth", Desc: "Moodle calendar from table to CSS grid",
		},
	}
}

func canvasMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Canvas LMS", PageID: "canvas_modules", PatternID: "module_list",
			OrigHTML:    `<div id="context_modules"><div class="context_module" data-module-id="1"><div class="header"><span class="name">Module 1: Introduction</span></div><div class="content"><ul class="context_module_items"><li class="context_module_item"><a href="/pages/welcome">Welcome Page</a></li></ul></div></div></div>`,
			MutatedHTML: `<div id="context_modules"><section class="module-container" data-module-id="1"><h2 class="module-title">Module 1: Introduction</h2><div class="module-items"><article class="module-item"><a href="/pages/welcome" class="item-link">Welcome Page</a></article></div></section></div>`,
			Selector:    "div.context_module", Desc: "Canvas module from div to semantic section/article",
		},
		{
			Site: "Canvas LMS", PageID: "canvas_assignments", PatternID: "assignment_list",
			OrigHTML:    `<div id="assignment_group_1"><div class="assignment_group"><div class="ig-header"><a class="ig-header-title">Homework</a></div><div class="ig-list"><div class="ig-row"><a class="ig-title" href="/assignments/1">Essay 1</a><span class="score">85/100</span></div></div></div></div>`,
			MutatedHTML: `<div id="assignment_group_1"><section class="assignment-group" role="region"><header><h3 class="group-name">Homework</h3></header><ul class="assignment-list"><li class="assignment-item"><a href="/assignments/1" class="assignment-name">Essay 1</a><span class="grade">85/100</span></li></ul></section></div>`,
			Selector:    "div.assignment_group", Desc: "Canvas assignment group to semantic section/list",
		},
		{
			Site: "Canvas LMS", PageID: "canvas_discussion", PatternID: "discussion_thread",
			OrigHTML:    `<div class="discussion-entries"><div class="entry"><div class="header"><span class="author">Instructor</span><span class="timestamp">Mar 15</span></div><div class="message">Welcome to the discussion</div></div></div>`,
			MutatedHTML: `<div class="discussion-entries"><article class="discussion-post" data-entry-id="1"><header class="post-header"><span class="poster-name">Instructor</span><time datetime="2026-03-15">Mar 15</time></header><div class="post-content">Welcome to the discussion</div></article></div>`,
			Selector:    "div.entry", Desc: "Canvas discussion entry from div to article",
		},
		{
			Site: "Canvas LMS", PageID: "canvas_grades", PatternID: "gradebook",
			OrigHTML:    `<table id="grades_summary"><thead><tr><th>Name</th><th>Score</th><th>Out of</th></tr></thead><tbody><tr class="student_assignment"><td>Essay 1</td><td class="grade">85</td><td>100</td></tr></tbody></table>`,
			MutatedHTML: `<div id="grades_summary" role="table"><div class="gradebook-header" role="row"><span role="columnheader">Name</span><span role="columnheader">Score</span><span role="columnheader">Out of</span></div><div class="grade-entry" role="row" data-assignment="essay1"><span>Essay 1</span><span class="score">85</span><span>100</span></div></div>`,
			Selector:    "table#grades_summary", Desc: "Canvas gradebook table to ARIA div grid",
		},
		{
			Site: "Canvas LMS", PageID: "canvas_files", PatternID: "file_list",
			OrigHTML:    `<div class="ef-main"><div class="ef-directory"><table class="ef-item-row"><tr><td class="ef-name-col"><a href="/files/1">lecture-01.pdf</a></td><td class="ef-date-col">Mar 10</td><td class="ef-size-col">2.3 MB</td></tr></table></div></div>`,
			MutatedHTML: `<div class="ef-main"><div class="file-browser"><div class="file-row" data-id="1"><span class="file-name"><a href="/files/1">lecture-01.pdf</a></span><span class="file-date">Mar 10</span><span class="file-size">2.3 MB</span></div></div></div>`,
			Selector:    "table.ef-item-row", Desc: "Canvas file browser from table to div rows",
		},
		{
			Site: "Canvas LMS", PageID: "canvas_quiz", PatternID: "quiz_question",
			OrigHTML:    `<div id="questions"><div class="question multiple_choice_question" data-id="q1"><div class="text"><p>Select the correct answer:</p></div><div class="answers"><label class="answer"><input type="radio" name="q1">Option A</label><label class="answer"><input type="radio" name="q1">Option B</label></div></div></div>`,
			MutatedHTML: `<div id="questions"><fieldset class="quiz-question" data-type="multiple_choice" data-id="q1"><legend class="question-text">Select the correct answer:</legend><div class="answer-choices"><label><input type="radio" name="q1" value="a">Option A</label><label><input type="radio" name="q1" value="b">Option B</label></div></fieldset></div>`,
			Selector:    "div.question.multiple_choice_question", Desc: "Canvas quiz from div to fieldset",
		},
	}
}

func shopifyMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Shopify", PageID: "shopify_product", PatternID: "product_card",
			OrigHTML:    `<div class="product-card"><a href="/products/item-1"><img src="/img/item.jpg" alt="Item"><div class="product-card__info"><h3>Product Name</h3><span class="price">$29.99</span></div></a></div>`,
			MutatedHTML: `<article class="product-item" data-product-id="1"><a href="/products/item-1" class="product-link"><picture><img src="/img/item.webp" alt="Item" loading="lazy"></picture><div class="product-details"><h3 class="product-title">Product Name</h3><div class="product-price" data-price="2999">$29.99</div></div></a></article>`,
			Selector:    "div.product-card", Desc: "Shopify product card to semantic article",
		},
		{
			Site: "Shopify", PageID: "shopify_cart", PatternID: "cart_items",
			OrigHTML:    `<table class="cart-items"><tbody><tr class="cart-item" data-id="1"><td><img src="/img/item.jpg" width="80"></td><td class="cart-item__name">Product Name</td><td class="cart-item__quantity"><input type="number" value="1"></td><td class="cart-item__price">$29.99</td></tr></tbody></table>`,
			MutatedHTML: `<div class="cart-items-list"><div class="cart-line-item" data-variant="1"><div class="line-item__image"><img src="/img/item.webp" width="80" loading="lazy"></div><div class="line-item__info"><p class="line-item__title">Product Name</p><quantity-input><input type="number" value="1" min="0"></quantity-input><span class="line-item__price">$29.99</span></div></div></div>`,
			Selector:    "table.cart-items", Desc: "Shopify cart table to div-based line items",
		},
		{
			Site: "Shopify", PageID: "shopify_collection", PatternID: "collection_filter",
			OrigHTML:    `<div class="collection-sidebar"><div class="filter-group"><h4>Color</h4><ul class="filter-list"><li><input type="checkbox" id="red"><label for="red">Red</label></li><li><input type="checkbox" id="blue"><label for="blue">Blue</label></li></ul></div></div>`,
			MutatedHTML: `<aside class="facets-container" role="complementary"><details class="facet" open><summary class="facet-title">Color</summary><fieldset class="facet-options"><label class="facet-option"><input type="checkbox" value="red">Red</label><label class="facet-option"><input type="checkbox" value="blue">Blue</label></fieldset></details></aside>`,
			Selector:    "div.collection-sidebar", Desc: "Shopify filter sidebar to details/summary",
		},
		{
			Site: "Shopify", PageID: "shopify_nav", PatternID: "header_nav",
			OrigHTML:    `<header><nav class="site-nav"><ul class="site-nav__list"><li class="site-nav__item"><a href="/collections">Shop</a></li><li class="site-nav__item"><a href="/pages/about">About</a></li></ul></nav></header>`,
			MutatedHTML: `<header class="header-wrapper"><nav class="header__nav" aria-label="Primary"><menu-drawer class="mobile-nav"><ul role="list"><li><a href="/collections" class="header__nav-link">Shop</a></li><li><a href="/pages/about" class="header__nav-link">About</a></li></ul></menu-drawer></nav></header>`,
			Selector:    "ul.site-nav__list", Desc: "Shopify nav to web component menu-drawer",
		},
	}
}

func drupalMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Drupal", PageID: "drupal_content", PatternID: "node_article",
			OrigHTML:    `<article class="node node--type-article"><h2 class="node__title"><a href="/node/1">Article Title</a></h2><div class="node__content"><div class="field field--name-body"><p>Article body content here</p></div></div></article>`,
			MutatedHTML: `<article class="node article" data-nid="1"><header class="node-header"><h2><a href="/node/1" class="node-title">Article Title</a></h2></header><div class="node-body"><div class="text-content"><p>Article body content here</p></div></div></article>`,
			Selector:    "article.node--type-article", Desc: "Drupal article BEM class to simpler naming",
		},
		{
			Site: "Drupal", PageID: "drupal_menu", PatternID: "main_menu",
			OrigHTML:    `<nav role="navigation" class="menu--main"><ul class="menu"><li class="menu-item"><a href="/" data-drupal-link-system-path="<front>">Home</a></li><li class="menu-item"><a href="/blog">Blog</a></li></ul></nav>`,
			MutatedHTML: `<nav role="navigation" aria-label="Main menu"><div class="navigation-menu"><a href="/" class="nav-link active" data-path="home">Home</a><a href="/blog" class="nav-link">Blog</a></div></nav>`,
			Selector:    "ul.menu", Desc: "Drupal menu from ul list to flat div links",
		},
		{
			Site: "Drupal", PageID: "drupal_views", PatternID: "views_table",
			OrigHTML:    `<div class="view view-content"><table class="views-table cols-3"><thead><tr><th>Title</th><th>Author</th><th>Date</th></tr></thead><tbody><tr class="odd"><td>Post 1</td><td>Admin</td><td>Mar 15</td></tr></tbody></table></div>`,
			MutatedHTML: `<div class="view view-content"><div class="views-list" role="list"><div class="views-row" role="listitem"><span class="views-field-title">Post 1</span><span class="views-field-author">Admin</span><span class="views-field-date">Mar 15</span></div></div></div>`,
			Selector:    "table.views-table", Desc: "Drupal views table to div list",
		},
		{
			Site: "Drupal", PageID: "drupal_form", PatternID: "contact_form",
			OrigHTML:    `<form class="contact-message-form" data-drupal-selector="contact-message-form"><div class="form-item"><label for="edit-name">Name</label><input type="text" id="edit-name" class="form-text"></div><div class="form-actions"><input type="submit" value="Send message" class="button button--primary"></div></form>`,
			MutatedHTML: `<form class="contact-form" data-drupal-selector="contact-message-form"><div class="form-element-container"><label for="edit-name" class="form-label">Name</label><input type="text" id="edit-name" class="form-element"></div><div class="form-actions"><button type="submit" class="button button--primary">Send message</button></div></form>`,
			Selector:    "input.button--primary", Desc: "Drupal form submit from input to button",
		},
	}
}

func extendedD2lMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "D2L Brightspace", PageID: "d2l_content", PatternID: "content_tree",
			OrigHTML:    `<div class="d2l-tree"><ul class="d2l-tree-list"><li class="d2l-tree-item"><a href="/content/1">Module 1</a><ul><li><a href="/content/1/page1">Page 1</a></li></ul></li></ul></div>`,
			MutatedHTML: `<d2l-hierarchical-view class="content-browser"><d2l-list><d2l-list-item><a href="/content/1">Module 1</a><d2l-list nested><d2l-list-item><a href="/content/1/page1">Page 1</a></d2l-list-item></d2l-list></d2l-list-item></d2l-list></d2l-hierarchical-view>`,
			Selector:    "ul.d2l-tree-list", Desc: "D2L content tree to web component hierarchy",
		},
		{
			Site: "D2L Brightspace", PageID: "d2l_dropbox", PatternID: "dropbox_upload",
			OrigHTML:    `<div class="d2l-dropbox"><div class="d2l-fileupload"><input type="file" id="fileInput" class="d2l-fileinput"><button class="d2l-button" onclick="upload()">Upload</button></div></div>`,
			MutatedHTML: `<div class="d2l-dropbox"><d2l-file-uploader accept="*/*" max-file-size="52428800"><d2l-button-subtle text="Upload File" icon="tier1:upload"></d2l-button-subtle></d2l-file-uploader></div>`,
			Selector:    "input#fileInput", Desc: "D2L file upload to web component uploader",
		},
		{
			Site: "D2L Brightspace", PageID: "d2l_rubric", PatternID: "rubric_grid",
			OrigHTML:    `<table class="d2l-rubric-table"><thead><tr><th>Criteria</th><th>Level 1</th><th>Level 2</th><th>Level 3</th></tr></thead><tbody><tr><td>Writing</td><td>Poor</td><td>Good</td><td class="selected">Excellent</td></tr></tbody></table>`,
			MutatedHTML: `<div class="d2l-rubric" role="grid"><div class="rubric-criteria-header" role="row"><span role="columnheader">Criteria</span><span role="columnheader">Level 1</span><span role="columnheader">Level 2</span><span role="columnheader">Level 3</span></div><div class="rubric-criteria-row" role="row"><span class="criteria-name">Writing</span><span class="level">Poor</span><span class="level">Good</span><span class="level selected" aria-selected="true">Excellent</span></div></div>`,
			Selector:    "table.d2l-rubric-table", Desc: "D2L rubric table to ARIA grid",
		},
	}
}

func extendedWordPressMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "WordPress", PageID: "wp_search", PatternID: "search_form",
			OrigHTML:    `<form role="search" class="search-form"><label for="s">Search</label><input type="search" id="s" class="search-field" placeholder="Search..."><button type="submit" class="search-submit">Search</button></form>`,
			MutatedHTML: `<form role="search" class="wp-block-search"><label class="wp-block-search__label" for="wp-block-search-1">Search</label><div class="wp-block-search__inside-wrapper"><input type="search" id="wp-block-search-1" class="wp-block-search__input" placeholder="Search..."><button type="submit" class="wp-block-search__button">Search</button></div></form>`,
			Selector:    "input#s.search-field", Desc: "WordPress classic search to block search",
		},
		{
			Site: "WordPress", PageID: "wp_sidebar", PatternID: "widget_area",
			OrigHTML:    `<aside id="secondary" class="widget-area"><section class="widget widget_recent_entries"><h2 class="widget-title">Recent Posts</h2><ul><li><a href="/post-1">Post 1</a></li><li><a href="/post-2">Post 2</a></li></ul></section></aside>`,
			MutatedHTML: `<aside id="secondary" class="wp-block-widget-area"><div class="wp-block-group widget"><h2 class="wp-block-heading">Recent Posts</h2><ul class="wp-block-latest-posts__list"><li><a href="/post-1">Post 1</a></li><li><a href="/post-2">Post 2</a></li></ul></div></aside>`,
			Selector:    "section.widget_recent_entries", Desc: "WordPress classic widget to block widget",
		},
		{
			Site: "WordPress", PageID: "wp_comments", PatternID: "comment_list",
			OrigHTML:    `<ol class="comment-list"><li class="comment"><article class="comment-body"><footer class="comment-meta"><span class="comment-author">John</span></footer><div class="comment-content"><p>Great post!</p></div></article></li></ol>`,
			MutatedHTML: `<div class="wp-block-comments"><div class="comment-thread"><div class="comment" data-id="1"><div class="comment-header"><span class="comment-author-name">John</span></div><div class="comment-body"><p>Great post!</p></div></div></div></div>`,
			Selector:    "ol.comment-list", Desc: "WordPress comment list from ol to div blocks",
		},
		{
			Site: "WordPress", PageID: "wp_gallery", PatternID: "image_gallery",
			OrigHTML:    `<div class="gallery gallery-columns-3"><figure class="gallery-item"><div class="gallery-icon"><a href="/img/1.jpg"><img src="/img/1-thumb.jpg" alt="Image 1"></a></div><figcaption>Caption 1</figcaption></figure></div>`,
			MutatedHTML: `<figure class="wp-block-gallery columns-3"><ul class="blocks-gallery-grid"><li class="blocks-gallery-item"><figure><a href="/img/1.jpg"><img src="/img/1-thumb.webp" alt="Image 1" loading="lazy"></a><figcaption>Caption 1</figcaption></figure></li></ul></figure>`,
			Selector:    "div.gallery", Desc: "WordPress classic gallery to block gallery",
		},
	}
}

func extendedUniversityCatalogMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "University Catalog", PageID: "catalog_program", PatternID: "program_detail",
			OrigHTML:    `<div class="program-detail"><h1 class="program-title">Computer Science BS</h1><div class="program-info"><div class="credits">Credits: 120</div><div class="department">Department: CS</div></div><div class="requirements"><h2>Core Requirements</h2><ul class="req-list"><li>CS101 - Intro to CS</li><li>CS201 - Data Structures</li></ul></div></div>`,
			MutatedHTML: `<article class="academic-program" data-program="cs-bs"><header><h1>Computer Science BS</h1><dl class="program-metadata"><dt>Credits</dt><dd>120</dd><dt>Department</dt><dd>CS</dd></dl></header><section class="curriculum"><h2>Core Requirements</h2><div class="course-list"><div class="course-item" data-course="cs101">CS101 - Intro to CS</div><div class="course-item" data-course="cs201">CS201 - Data Structures</div></div></section></article>`,
			Selector:    "div.program-detail", Desc: "Catalog program from div to semantic article",
		},
		{
			Site: "University Catalog", PageID: "catalog_schedule", PatternID: "class_schedule",
			OrigHTML:    `<table id="class-schedule"><thead><tr><th>Course</th><th>Section</th><th>Days</th><th>Time</th><th>Room</th></tr></thead><tbody><tr><td>CS101</td><td>001</td><td>MWF</td><td>9:00-9:50</td><td>Science 101</td></tr></tbody></table>`,
			MutatedHTML: `<div id="class-schedule" class="schedule-grid" role="table"><div class="schedule-header" role="row"><span role="columnheader">Course</span><span role="columnheader">Section</span><span role="columnheader">Days</span><span role="columnheader">Time</span><span role="columnheader">Room</span></div><div class="schedule-row" role="row" data-crn="12345"><span>CS101</span><span>001</span><span>MWF</span><span>9:00-9:50</span><span>Science 101</span></div></div>`,
			Selector:    "table#class-schedule", Desc: "Catalog class schedule table to div grid",
		},
		{
			Site: "University Catalog", PageID: "catalog_faculty", PatternID: "faculty_list",
			OrigHTML:    `<div class="faculty-listing"><div class="faculty-member"><img src="/img/prof.jpg" class="faculty-photo"><h3>Dr. Smith</h3><p class="title">Professor</p><p class="email">smith@university.edu</p></div></div>`,
			MutatedHTML: `<section class="faculty-directory"><article class="faculty-card" data-id="smith"><div class="card-avatar"><img src="/img/prof.webp" alt="Dr. Smith" loading="lazy"></div><div class="card-info"><h3 class="faculty-name">Dr. Smith</h3><span class="faculty-title">Professor</span><a class="faculty-email" href="mailto:smith@university.edu">smith@university.edu</a></div></article></section>`,
			Selector:    "div.faculty-member", Desc: "Catalog faculty listing from div to article card",
		},
	}
}

func joomlaMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Joomla", PageID: "joomla_article", PatternID: "article_content",
			OrigHTML:    `<div class="item-page"><div class="page-header"><h2>Article Title</h2></div><div class="article-info"><span class="createdby">By Admin</span></div><div class="article-body"><p>Content here</p></div></div>`,
			MutatedHTML: `<article class="com-content-article" itemscope><header><h2 itemprop="name">Article Title</h2><div class="article-meta"><span class="author" itemprop="author">By Admin</span></div></header><section class="article-content" itemprop="articleBody"><p>Content here</p></section></article>`,
			Selector:    "div.item-page", Desc: "Joomla article to semantic + microdata",
		},
		{
			Site: "Joomla", PageID: "joomla_menu", PatternID: "main_nav",
			OrigHTML:    `<nav class="navbar"><ul class="nav menu"><li class="item-101"><a href="/">Home</a></li><li class="item-102"><a href="/about">About</a></li></ul></nav>`,
			MutatedHTML: `<nav class="navbar" aria-label="Main"><div class="navbar-nav"><a class="nav-link" href="/" data-item="101">Home</a><a class="nav-link" href="/about" data-item="102">About</a></div></nav>`,
			Selector:    "ul.nav.menu", Desc: "Joomla nav from ul to flat div links",
		},
		{
			Site: "Joomla", PageID: "joomla_login", PatternID: "login_form",
			OrigHTML:    `<form class="form-login"><div class="control-group"><label for="modlgn-username">Username</label><input type="text" id="modlgn-username" class="input-small"></div><div class="control-group"><label for="modlgn-passwd">Password</label><input type="password" id="modlgn-passwd" class="input-small"></div><button type="submit" class="btn btn-primary">Log in</button></form>`,
			MutatedHTML: `<form class="mod-login" data-module="login"><div class="mod-login__input"><label for="login-username">Username</label><input type="text" id="login-username" class="form-control"></div><div class="mod-login__input"><label for="login-password">Password</label><input type="password" id="login-password" class="form-control"></div><button type="submit" class="mod-login__submit btn btn-primary">Log in</button></form>`,
			Selector:    "input#modlgn-username", Desc: "Joomla login form ID and class rename",
		},
	}
}

func spaMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "React SPA", PageID: "spa_dashboard", PatternID: "dashboard_widgets",
			OrigHTML:    `<div id="root"><div class="dashboard"><div class="widget-grid"><div class="widget" data-id="revenue"><h3>Revenue</h3><span class="value">$12,345</span></div><div class="widget" data-id="users"><h3>Users</h3><span class="value">1,234</span></div></div></div></div>`,
			MutatedHTML: `<div id="root"><main class="dashboard-view"><section class="metrics-grid" role="region" aria-label="Metrics"><article class="metric-card" data-metric="revenue"><h3 class="metric-title">Revenue</h3><div class="metric-value" aria-live="polite">$12,345</div></article><article class="metric-card" data-metric="users"><h3 class="metric-title">Users</h3><div class="metric-value" aria-live="polite">1,234</div></article></section></main></div>`,
			Selector:    "div.widget-grid", Desc: "React dashboard widget grid to semantic articles",
		},
		{
			Site: "React SPA", PageID: "spa_table", PatternID: "data_table",
			OrigHTML:    `<div class="table-container"><table class="data-table"><thead><tr><th>Name</th><th>Email</th><th>Role</th></tr></thead><tbody><tr><td>Alice</td><td>alice@example.com</td><td>Admin</td></tr></tbody></table></div>`,
			MutatedHTML: `<div class="table-container"><div class="virtual-table" role="table"><div class="table-header" role="rowgroup"><div role="row"><span role="columnheader">Name</span><span role="columnheader">Email</span><span role="columnheader">Role</span></div></div><div class="table-body" role="rowgroup"><div role="row" data-index="0"><span>Alice</span><span>alice@example.com</span><span>Admin</span></div></div></div></div>`,
			Selector:    "table.data-table", Desc: "React table to virtual scrolling div grid",
		},
		{
			Site: "React SPA", PageID: "spa_modal", PatternID: "modal_dialog",
			OrigHTML:    `<div class="modal-overlay"><div class="modal"><div class="modal-header"><h2>Confirm Action</h2><button class="close-btn">&times;</button></div><div class="modal-body"><p>Are you sure?</p></div><div class="modal-footer"><button class="btn cancel">Cancel</button><button class="btn confirm">Confirm</button></div></div></div>`,
			MutatedHTML: `<div class="modal-backdrop" role="presentation"><dialog class="dialog-modal" open role="alertdialog" aria-modal="true"><header class="dialog-header"><h2 id="dialog-title">Confirm Action</h2><button class="dialog-close" aria-label="Close">&times;</button></header><div class="dialog-content"><p>Are you sure?</p></div><footer class="dialog-actions"><button class="action-secondary" data-action="cancel">Cancel</button><button class="action-primary" data-action="confirm">Confirm</button></footer></dialog></div>`,
			Selector:    "div.modal", Desc: "React modal div to native dialog element",
		},
		{
			Site: "React SPA", PageID: "spa_sidebar", PatternID: "app_sidebar",
			OrigHTML:    `<div class="sidebar"><div class="sidebar-header"><h2>Navigation</h2></div><ul class="sidebar-menu"><li class="menu-item active"><a href="/dashboard"><i class="icon-home"></i>Dashboard</a></li><li class="menu-item"><a href="/settings"><i class="icon-gear"></i>Settings</a></li></ul></div>`,
			MutatedHTML: `<aside class="app-sidebar" role="navigation" aria-label="App navigation"><div class="sidebar-brand"><h2>Navigation</h2></div><nav class="sidebar-nav"><a href="/dashboard" class="nav-item active" aria-current="page"><svg class="nav-icon"><use href="#icon-home"></use></svg><span>Dashboard</span></a><a href="/settings" class="nav-item"><svg class="nav-icon"><use href="#icon-gear"></use></svg><span>Settings</span></a></nav></aside>`,
			Selector:    "ul.sidebar-menu", Desc: "React sidebar from ul to flat nav links",
		},
	}
}

func additionalMoodleMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Moodle", PageID: "moodle_activity", PatternID: "activity_chooser",
			OrigHTML:    `<div class="choosercontainer"><div class="alloptions"><div class="option"><label><input type="radio" name="activity" value="assign"><span class="typename">Assignment</span></label></div><div class="option"><label><input type="radio" name="activity" value="quiz"><span class="typename">Quiz</span></label></div></div></div>`,
			MutatedHTML: `<div class="activity-chooser" role="listbox"><div class="chooser-item" role="option" data-type="assign"><div class="activity-icon"><img src="/mod/assign/icon.svg" alt=""></div><span class="activity-name">Assignment</span></div><div class="chooser-item" role="option" data-type="quiz"><div class="activity-icon"><img src="/mod/quiz/icon.svg" alt=""></div><span class="activity-name">Quiz</span></div></div>`,
			Selector:    "div.alloptions", Desc: "Moodle activity chooser to ARIA listbox",
		},
		{
			Site: "Moodle", PageID: "moodle_enrol", PatternID: "enrol_form",
			OrigHTML:    `<div class="enrolmentform"><form id="enrol_self_enrol_form"><input type="hidden" name="id" value="2"><label for="enrolpassword">Enrolment key</label><input type="password" id="enrolpassword" class="form-control"><input type="submit" value="Enrol me" class="btn btn-primary"></form></div>`,
			MutatedHTML: `<div class="enrol-container"><form id="enrol_self_enrol_form" data-enhance="moodle-core"><fieldset><legend class="sr-only">Self-enrolment</legend><input type="hidden" name="id" value="2"><div class="form-group"><label for="enrolpassword" class="form-label">Enrolment key</label><input type="password" id="enrolpassword" class="form-control"></div><button type="submit" class="btn btn-primary">Enrol me</button></fieldset></form></div>`,
			Selector:    "input[type=submit][value='Enrol me']", Desc: "Moodle enrol submit from input to button",
		},
	}
}

func additionalCanvasMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Canvas LMS", PageID: "canvas_announcements", PatternID: "announcement_list",
			OrigHTML:    `<div id="announcements_list"><div class="discussion-topic"><div class="discussion-title"><a href="/announcements/1">Welcome to the course</a></div><div class="discussion-summary"><p>This is the first announcement</p></div></div></div>`,
			MutatedHTML: `<div id="announcements_list"><article class="announcement-item" data-id="1"><header><h3 class="announcement-title"><a href="/announcements/1">Welcome to the course</a></h3></header><div class="announcement-body"><p>This is the first announcement</p></div></article></div>`,
			Selector:    "div.discussion-topic", Desc: "Canvas announcement from div to article",
		},
		{
			Site: "Canvas LMS", PageID: "canvas_people", PatternID: "roster_table",
			OrigHTML:    `<table class="roster"><thead><tr><th>Name</th><th>Section</th><th>Role</th></tr></thead><tbody><tr class="rosterUser"><td><a href="/users/1">Alice Student</a></td><td>Section A</td><td>Student</td></tr></tbody></table>`,
			MutatedHTML: `<div class="roster-container" role="table"><div class="roster-header" role="row"><span role="columnheader">Name</span><span role="columnheader">Section</span><span role="columnheader">Role</span></div><div class="roster-entry" role="row" data-user-id="1"><span><a href="/users/1">Alice Student</a></span><span>Section A</span><span>Student</span></div></div>`,
			Selector:    "table.roster", Desc: "Canvas roster table to ARIA div grid",
		},
	}
}

func additionalShopifyMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Shopify", PageID: "shopify_footer", PatternID: "footer_links",
			OrigHTML:    `<footer class="site-footer"><div class="footer-block"><h4>Quick Links</h4><ul class="footer-menu"><li><a href="/pages/faq">FAQ</a></li><li><a href="/pages/returns">Returns</a></li></ul></div></footer>`,
			MutatedHTML: `<footer class="footer section-footer"><div class="footer__content-top"><h4 class="footer__heading">Quick Links</h4><nav class="footer__list-wrap"><ul class="footer__list" role="list"><li><a href="/pages/faq" class="footer__list-item link">FAQ</a></li><li><a href="/pages/returns" class="footer__list-item link">Returns</a></li></ul></nav></div></footer>`,
			Selector:    "ul.footer-menu", Desc: "Shopify footer menu with new theme classes",
		},
		{
			Site: "Shopify", PageID: "shopify_announcement", PatternID: "announcement_bar",
			OrigHTML:    `<div class="announcement-bar"><div class="announcement-bar__message"><p>Free shipping on orders over $50</p></div></div>`,
			MutatedHTML: `<div class="utility-bar" role="region" aria-label="Announcement"><div class="announcement-bar__wrapper"><marquee-text class="announcement-bar__text"><span class="announcement-bar__message">Free shipping on orders over $50</span></marquee-text></div></div>`,
			Selector:    "div.announcement-bar__message p", Desc: "Shopify announcement bar to web component",
		},
	}
}

func additionalDrupalMutations() []SiteMutation {
	return []SiteMutation{
		{
			Site: "Drupal", PageID: "drupal_breadcrumb", PatternID: "breadcrumb_nav",
			OrigHTML:    `<nav class="breadcrumb" role="navigation"><h2 class="visually-hidden">Breadcrumb</h2><ol><li><a href="/">Home</a></li><li><a href="/blog">Blog</a></li><li>Current Page</li></ol></nav>`,
			MutatedHTML: `<nav class="breadcrumb" role="navigation" aria-label="Breadcrumb"><div class="breadcrumb__list"><a href="/" class="breadcrumb__link">Home</a><span class="breadcrumb__separator" aria-hidden="true">/</span><a href="/blog" class="breadcrumb__link">Blog</a><span class="breadcrumb__separator" aria-hidden="true">/</span><span class="breadcrumb__current" aria-current="page">Current Page</span></div></nav>`,
			Selector:    "nav.breadcrumb ol", Desc: "Drupal breadcrumb from ol to flat div",
		},
	}
}

func allExpandedMutations() []SiteMutation {
	var all []SiteMutation
	all = append(all, d2lMutations()...)
	all = append(all, extendedD2lMutations()...)
	all = append(all, wordpressMutations()...)
	all = append(all, extendedWordPressMutations()...)
	all = append(all, universityCatalogMutations()...)
	all = append(all, extendedUniversityCatalogMutations()...)
	all = append(all, moodleMutations()...)
	all = append(all, additionalMoodleMutations()...)
	all = append(all, canvasMutations()...)
	all = append(all, additionalCanvasMutations()...)
	all = append(all, shopifyMutations()...)
	all = append(all, additionalShopifyMutations()...)
	all = append(all, drupalMutations()...)
	all = append(all, additionalDrupalMutations()...)
	all = append(all, joomlaMutations()...)
	all = append(all, spaMutations()...)
	return all
}

func TestCrossSite_50PlusMutationScenarios(t *testing.T) {
	mutations := allExpandedMutations()
	require.GreaterOrEqual(t, len(mutations), 50,
		"need 50+ mutation scenarios; got %d", len(mutations))

	dir := t.TempDir()
	tracker, err := NewPatternTracker(dir+"/patterns.json", dir)
	require.NoError(t, err)
	ctx := context.Background()
	pp := NewPatternPipeline(tracker, nil)

	siteStats := make(map[string]struct{ total, detected int })

	for _, m := range mutations {
		err := tracker.RegisterPattern(ctx, m.PatternID, m.Selector, m.Desc, m.OrigHTML)
		require.NoError(t, err)

		pp.CheckAndAlert(ctx, m.PageID, m.PatternID, m.OrigHTML)
		drifted, _ := pp.CheckAndAlert(ctx, m.PageID, m.PatternID, m.MutatedHTML)

		stats := siteStats[m.Site]
		stats.total++
		if drifted {
			stats.detected++
		}
		siteStats[m.Site] = stats
	}

	for site, stats := range siteStats {
		rate := float64(stats.detected) / float64(stats.total)
		t.Logf("%-20s: %d/%d detected (%.0f%%)", site, stats.detected, stats.total, rate*100)
		assert.GreaterOrEqual(t, rate, 0.8,
			"site %s should detect >=80%% of mutations", site)
	}
}

func TestCrossSite_SeverityDistribution(t *testing.T) {
	mutations := allExpandedMutations()
	sevCounts := map[DriftSeverity]int{}

	for _, m := range mutations {
		origFP := domheal.ParseDOMFingerprint(m.OrigHTML)
		mutFP := domheal.ParseDOMFingerprint(m.MutatedHTML)
		sim := domheal.DOMFingerprintSimilarity(origFP, mutFP)
		sev := ClassifyDriftSeverity(sim)
		sevCounts[sev]++
	}

	t.Logf("Severity distribution across %d mutations:", len(mutations))
	for _, sev := range []DriftSeverity{DriftSeverityLow, DriftSeverityMedium, DriftSeverityHigh, DriftSeverityCritical} {
		t.Logf("  %-10s: %d", sev, sevCounts[sev])
	}

	assert.Greater(t, sevCounts[DriftSeverityMedium]+sevCounts[DriftSeverityHigh], 0,
		"should have medium or high severity mutations")
}

func TestCrossSite_PatternTransferLearning(t *testing.T) {
	dir := t.TempDir()
	tracker, err := NewPatternTracker(dir+"/patterns.json", dir)
	require.NoError(t, err)
	ctx := context.Background()

	d2lNav := d2lMutations()[1]
	err = tracker.RegisterPattern(ctx, "d2l_nav_pattern", d2lNav.Selector, d2lNav.Desc, d2lNav.OrigHTML)
	require.NoError(t, err)

	moodleNav := moodleMutations()[3]
	err = tracker.RegisterPattern(ctx, "moodle_nav_pattern", moodleNav.Selector, moodleNav.Desc, moodleNav.OrigHTML)
	require.NoError(t, err)

	d2lNavFP := domheal.ParseDOMFingerprint(d2lNav.OrigHTML)
	moodleNavFP := domheal.ParseDOMFingerprint(moodleNav.OrigHTML)
	crossSiteSim := domheal.DOMFingerprintSimilarity(d2lNavFP, moodleNavFP)

	t.Logf("Cross-site nav similarity (D2L <-> Moodle): %.3f", crossSiteSim)
	assert.Greater(t, crossSiteSim, 0.2,
		"navigation patterns from different LMS should share some structural similarity")

	d2lGrades := d2lMutations()[0]
	canvasGrades := canvasMutations()[3]
	d2lGradesFP := domheal.ParseDOMFingerprint(d2lGrades.OrigHTML)
	canvasGradesFP := domheal.ParseDOMFingerprint(canvasGrades.OrigHTML)
	gradeSim := domheal.DOMFingerprintSimilarity(d2lGradesFP, canvasGradesFP)

	t.Logf("Cross-site grade table similarity (D2L <-> Canvas): %.3f", gradeSim)
	assert.Greater(t, gradeSim, 0.15,
		"grade tables from different LMS should share some structural similarity")
}

func TestCrossSite_FallbackChainAcrossPlatforms(t *testing.T) {
	mutations := allExpandedMutations()
	handoffs := NewInMemoryHandoffStore()
	lb := NewLatencyBudget(DefaultTierBudgets())
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return true },
		WithFallbackBudget(lb),
		WithFallbackHandoffs(handoffs),
	)

	tierUsage := map[ModelTier]int{}
	for _, m := range mutations {
		origFP := domheal.ParseDOMFingerprint(m.OrigHTML)
		mutFP := domheal.ParseDOMFingerprint(m.MutatedHTML)
		sim := domheal.DOMFingerprintSimilarity(origFP, mutFP)

		tier, err := fc.Execute(context.Background(), m.PatternID, func(ctx context.Context, tier ModelTier) error {
			switch tier {
			case TierLight:
				if sim < 0.5 {
					return &healError{msg: "light cannot handle major change"}
				}
				return nil
			case TierSmart:
				if sim < 0.15 {
					return &healError{msg: "smart cannot handle critical change"}
				}
				return nil
			default:
				return nil
			}
		})
		require.NoError(t, err)
		tierUsage[tier]++
	}

	t.Logf("Tier usage across %d mutations:", len(mutations))
	for _, tier := range []ModelTier{TierLight, TierSmart, TierVLM} {
		t.Logf("  %-8s: %d", tier, tierUsage[tier])
	}
	t.Logf("Total handoffs: %d", len(handoffs.Recent(200)))

	assert.Greater(t, tierUsage[TierLight], 0, "light tier should handle some mutations")
	assert.Greater(t, tierUsage[TierSmart], 0, "smart tier should handle escalated mutations")
}

func TestCrossSite_FingerprintMatcherAcrossPlatforms(t *testing.T) {
	matcher := domheal.NewFingerprintMatcher(0.6, nil)

	mutations := allExpandedMutations()
	for _, m := range mutations {
		result := matcher.CheckAndUpdate(m.PageID, m.OrigHTML)
		assert.True(t, result.IsNew, "first visit to %s should be new", m.PageID)
	}

	assert.GreaterOrEqual(t, matcher.KnownCount(), 50)

	driftCount := 0
	for _, m := range mutations {
		result := matcher.CheckAndUpdate(m.PageID, m.MutatedHTML)
		if result.Drifted {
			driftCount++
		}
	}

	rate := float64(driftCount) / float64(len(mutations))
	t.Logf("FingerprintMatcher drift detection: %d/%d (%.0f%%)", driftCount, len(mutations), rate*100)
	assert.GreaterOrEqual(t, rate, 0.7, "matcher should detect >=70%% drift across platforms")
}

func TestCrossSite_PlatformCount(t *testing.T) {
	mutations := allExpandedMutations()
	platforms := make(map[string]bool)
	for _, m := range mutations {
		platforms[m.Site] = true
	}

	t.Logf("Platforms covered: %d", len(platforms))
	for site := range platforms {
		t.Logf("  - %s", site)
	}

	assert.GreaterOrEqual(t, len(platforms), 5, "should cover at least 5 CMS platforms")
}
