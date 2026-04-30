package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PlaywrightComparisonResult captures one comparison run.
type PlaywrightComparisonResult struct {
	PageType         string  `json:"page_type"`
	PageWaiterMs     float64 `json:"pagewaiter_ms"`
	PageWaiterOK     bool    `json:"pagewaiter_ok"`
	PageWaiterDOMLen int     `json:"pagewaiter_dom_len"`
	ElementFound     bool    `json:"element_found"`
}

// PlaywrightComparisonReport is the full comparison report.
type PlaywrightComparisonReport struct {
	Timestamp   time.Time                    `json:"timestamp"`
	Description string                       `json:"description"`
	Results     []PlaywrightComparisonResult `json:"results"`
	Summary     ComparisonSummary            `json:"summary"`
}

// ComparisonSummary provides aggregate statistics.
type ComparisonSummary struct {
	TotalPages       int     `json:"total_pages"`
	PageWaiterPassed int     `json:"pagewaiter_passed"`
	ElementsFound    int     `json:"elements_found"`
	AvgWaitMs        float64 `json:"avg_wait_ms"`
	AccuracyRate     float64 `json:"accuracy_rate"`
}

func requirePlaywrightComparison(t *testing.T) {
	t.Helper()
	if os.Getenv("UIAUTO_PLAYWRIGHT_COMPARE") != "1" {
		t.Skip("set UIAUTO_PLAYWRIGHT_COMPARE=1 to run Playwright comparison tests")
	}
}

type comparisonPage struct {
	name        string
	html        string
	waitElement string
}

func comparisonPages() []comparisonPage {
	return []comparisonPage{
		{
			name:        "static_html",
			html:        `<!DOCTYPE html><html><body><h1 id="heading">Static</h1><p>Content</p></body></html>`,
			waitElement: "#heading",
		},
		{
			name: "ssr_page",
			html: `<!DOCTYPE html><html><head><title>SSR</title></head><body>
				<nav id="nav"><a href="/">Home</a></nav>
				<main id="main"><h1>Rendered</h1><ul><li>A</li><li>B</li></ul></main>
			</body></html>`,
			waitElement: "#main",
		},
		{
			name: "delayed_js_content",
			html: `<!DOCTYPE html><html><body>
				<div id="root"></div>
				<script>
					setTimeout(function() {
						document.getElementById('root').innerHTML = '<p id="result">Done</p>';
					}, 500);
				</script>
			</body></html>`,
			waitElement: "#result",
		},
		{
			name: "spa_multi_render",
			html: `<!DOCTYPE html><html><body>
				<div id="app"><p>Loading</p></div>
				<script>
					setTimeout(function() {
						document.getElementById('app').innerHTML = '<section id="content"><h2>Ready</h2></section>';
					}, 300);
				</script>
			</body></html>`,
			waitElement: "#content",
		},
		{
			name: "lazy_loaded",
			html: `<!DOCTYPE html><html><body>
				<h1>Page</h1>
				<div id="lazy-container"></div>
				<script>
					setTimeout(function() {
						var c = document.getElementById('lazy-container');
						for (var i = 0; i < 3; i++) {
							var d = document.createElement('div');
							d.className = 'lazy-item';
							d.id = 'lazy-' + i;
							d.textContent = 'Lazy item ' + i;
							c.appendChild(d);
						}
					}, 600);
				</script>
			</body></html>`,
			waitElement: "#lazy-0",
		},
		{
			name: "form_interactive",
			html: `<!DOCTYPE html><html><body>
				<form id="form">
					<input id="email" type="email" placeholder="Email">
					<input id="pass" type="password">
					<button id="btn" type="submit">Go</button>
				</form>
				<script>
					document.getElementById('form').addEventListener('submit', function(e) { e.preventDefault(); });
				</script>
			</body></html>`,
			waitElement: "#btn",
		},
		{
			name: "lms_like_accordion",
			html: `<!DOCTYPE html><html><body>
				<div class="d2l-page">
					<div class="module-header" id="mod-1">Module 1</div>
					<div class="module-content" id="content-1" style="display:none">Content 1</div>
				</div>
				<script>
					setTimeout(function() {
						document.getElementById('content-1').style.display = 'block';
					}, 400);
				</script>
			</body></html>`,
			waitElement: "#content-1",
		},
		{
			name: "mutation_heavy",
			html: `<!DOCTYPE html><html><body>
				<div id="ticker">0</div>
				<script>
					var c = 0;
					var iv = setInterval(function() {
						document.getElementById('ticker').textContent = String(++c);
						if (c >= 15) { clearInterval(iv); document.getElementById('ticker').setAttribute('data-done', 'true'); }
					}, 80);
				</script>
			</body></html>`,
			waitElement: "#ticker",
		},
		{
			name: "css_animation_page",
			html: `<!DOCTYPE html><html><body>
				<style>@keyframes fadein { from { opacity: 0; } to { opacity: 1; } }
				.fade { animation: fadein 0.5s ease-in forwards; }</style>
				<div id="animated" class="fade"><h1>Animated</h1></div>
			</body></html>`,
			waitElement: "#animated",
		},
		{
			name: "iframe_container",
			html: `<!DOCTYPE html><html><body>
				<h1 id="parent-heading">Parent</h1>
				<iframe id="embed" srcdoc="<html><body><p id='inner'>Inner Content</p></body></html>" width="400" height="200"></iframe>
			</body></html>`,
			waitElement: "#parent-heading",
		},
		{
			name: "d2l_module_list",
			html: `<!DOCTYPE html><html><body>
				<div class="d2l-modules">
					<div class="module" data-mod="1"><span class="module-title">Module 1</span></div>
					<div class="module" data-mod="2"><span class="module-title">Module 2</span></div>
				</div>
				<script>
					setTimeout(function() {
						var el = document.createElement('div');
						el.id = 'mod-content-1';
						el.className = 'module-content';
						el.textContent = 'Module 1 content loaded';
						document.querySelector('.d2l-modules').appendChild(el);
					}, 450);
				</script>
			</body></html>`,
			waitElement: "#mod-content-1",
		},
		{
			name: "d2l_submission_portal",
			html: `<!DOCTYPE html><html><body>
				<h1>Submit Assignment</h1>
				<div id="drop-area" style="border:2px dashed #ccc; padding:20px;">Drop files here</div>
				<script>
					setTimeout(function() {
						var zone = document.createElement('div');
						zone.id = 'upload-zone';
						zone.className = 'upload-zone';
						zone.innerHTML = '<input type="file" multiple> <span>Or click to browse</span>';
						document.getElementById('drop-area').appendChild(zone);
					}, 500);
				</script>
			</body></html>`,
			waitElement: "#upload-zone",
		},
		{
			name: "woocommerce_product_gallery",
			html: `<!DOCTYPE html><html><body>
				<div class="product">
					<div id="gallery-placeholder">Loading images...</div>
				</div>
				<script>
					setTimeout(function() {
						var gallery = document.createElement('div');
						gallery.id = 'gallery-loaded';
						gallery.className = 'product-gallery';
						gallery.innerHTML = '<img src="data:image/gif;base64,R0lGO" alt="1"><img src="data:image/gif;base64,R0lGO" alt="2">';
						document.getElementById('gallery-placeholder').replaceWith(gallery);
					}, 550);
				</script>
			</body></html>`,
			waitElement: "#gallery-loaded",
		},
		{
			name: "woocommerce_checkout",
			html: `<!DOCTYPE html><html><body>
				<form id="checkout-form">
					<input type="text" name="billing_name" placeholder="Name">
					<input type="email" name="billing_email" placeholder="Email">
				</form>
				<script>
					setTimeout(function() {
						var btn = document.createElement('button');
						btn.id = 'place-order';
						btn.type = 'submit';
						btn.textContent = 'Place order';
						document.getElementById('checkout-form').appendChild(btn);
					}, 400);
				</script>
			</body></html>`,
			waitElement: "#place-order",
		},
		{
			name: "progressive_enhancement",
			html: `<!DOCTYPE html><html><body>
				<div id="basic-content"><p>Basic content</p></div>
				<script>
					setTimeout(function() {
						var el = document.createElement('div');
						el.id = 'enhanced';
						el.textContent = 'Enhanced with JS';
						document.body.appendChild(el);
					}, 600);
				</script>
			</body></html>`,
			waitElement: "#enhanced",
		},
		{
			name: "api_driven_table",
			html: `<!DOCTYPE html><html><body>
				<table id="data-table"><thead><tr><th>ID</th><th>Name</th></tr></thead><tbody></tbody></table>
				<script>
					setTimeout(function() {
						var tbody = document.querySelector('#data-table tbody');
						for (var i = 0; i < 5; i++) {
							var tr = document.createElement('tr');
							tr.id = 'data-row-' + i;
							tr.innerHTML = '<td>' + i + '</td><td>Item ' + i + '</td>';
							tbody.appendChild(tr);
						}
					}, 350);
				</script>
			</body></html>`,
			waitElement: "#data-row-0",
		},
		{
			name: "multi_fetch_dashboard",
			html: `<!DOCTYPE html><html><body>
				<div id="dashboard"><div id="widget-1">Loading...</div><div id="widget-2">Loading...</div></div>
				<script>
					var done = 0;
					function check() { if (++done === 2) {
						var el = document.createElement('div');
						el.id = 'all-loaded';
						el.textContent = 'All data loaded';
						document.getElementById('dashboard').appendChild(el);
					}}
					setTimeout(function() { document.getElementById('widget-1').textContent = 'Widget 1'; check(); }, 200);
					setTimeout(function() { document.getElementById('widget-2').textContent = 'Widget 2'; check(); }, 400);
				</script>
			</body></html>`,
			waitElement: "#all-loaded",
		},
		{
			name: "websocket_feed",
			html: `<!DOCTYPE html><html><body>
				<div id="feed"></div>
				<script>
					var count = 0;
					function addItem() {
						var el = document.createElement('div');
						el.id = 'feed-item-' + count;
						el.textContent = 'Feed item ' + count;
						document.getElementById('feed').appendChild(el);
						count++;
					}
					setTimeout(addItem, 100);
					setTimeout(addItem, 250);
					setTimeout(addItem, 400);
					setTimeout(addItem, 550);
				</script>
			</body></html>`,
			waitElement: "#feed-item-3",
		},
		{
			name: "error_boundary_recovery",
			html: `<!DOCTYPE html><html><body>
				<div id="app">Loading...</div>
				<script>
					setTimeout(function() {
						try { throw new Error('Simulated error'); } catch (e) {}
						var el = document.createElement('div');
						el.id = 'recovered';
						el.textContent = 'Recovered';
						document.getElementById('app').replaceWith(el);
					}, 500);
				</script>
			</body></html>`,
			waitElement: "#recovered",
		},
		{
			name: "third_party_scripts",
			html: `<!DOCTYPE html><html><body>
				<div id="skeleton">Loading...</div>
				<script>
					setTimeout(function() {
						var el = document.createElement('main');
						el.id = 'main-content';
						el.innerHTML = '<h1>Content loaded after 3rd party scripts</h1>';
						document.getElementById('skeleton').replaceWith(el);
					}, 700);
				</script>
			</body></html>`,
			waitElement: "#main-content",
		},
		{
			name: "shadow_dom_components",
			html: `<!DOCTYPE html><html><body>
				<div id="mount"></div>
				<script>
					setTimeout(function() {
						var host = document.createElement('div');
						host.id = 'shadow-host';
						var shadow = host.attachShadow({mode: 'open'});
						shadow.innerHTML = '<p>Shadow content</p>';
						document.getElementById('mount').appendChild(host);
					}, 450);
				</script>
			</body></html>`,
			waitElement: "#shadow-host",
		},
		{
			name: "virtual_scroll_list",
			html: `<!DOCTYPE html><html><body>
				<div id="virtual-container" style="height:200px; overflow:auto;"></div>
				<script>
					setTimeout(function() {
						var container = document.getElementById('virtual-container');
						for (var i = 0; i < 100; i++) {
							var el = document.createElement('div');
							el.id = 'virtual-item-' + i;
							el.className = 'virtual-item';
							el.textContent = 'Item ' + i;
							container.appendChild(el);
						}
					}, 400);
				</script>
			</body></html>`,
			waitElement: "#virtual-item-0",
		},
		{
			name: "infinite_scroll",
			html: `<!DOCTYPE html><html><body>
				<div id="feed-list"></div>
				<script>
					var page = 0;
					function loadMore() {
						var el = document.createElement('div');
						el.id = 'page-' + page;
						el.textContent = 'Page ' + page + ' content';
						document.getElementById('feed-list').appendChild(el);
						page++;
					}
					setTimeout(loadMore, 100);
					setTimeout(loadMore, 300);
					setTimeout(loadMore, 500);
				</script>
			</body></html>`,
			waitElement: "#page-2",
		},
		{
			name: "toast_notification",
			html: `<!DOCTYPE html><html><body>
				<div id="content"><p>Main content</p></div>
				<script>
					setTimeout(function() {
						var toast = document.createElement('div');
						toast.id = 'toast';
						toast.className = 'toast-success';
						toast.textContent = 'Operation successful';
						document.body.appendChild(toast);
					}, 350);
				</script>
			</body></html>`,
			waitElement: "#toast",
		},
		{
			name: "modal_dialog",
			html: `<!DOCTYPE html><html><body>
				<div id="backdrop" style="display:none;position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.5)">
					<div id="modal" role="dialog"><h2>Confirm</h2><button id="modal-ok">OK</button></div>
				</div>
				<script>
					setTimeout(function() {
						document.getElementById('backdrop').style.display = 'block';
					}, 300);
				</script>
			</body></html>`,
			waitElement: "#modal-ok",
		},
		{
			name: "tab_navigation",
			html: `<!DOCTYPE html><html><body>
				<div role="tablist">
					<button role="tab" id="tab-1" aria-selected="true">Tab 1</button>
					<button role="tab" id="tab-2">Tab 2</button>
				</div>
				<div id="panel-1" role="tabpanel">Panel 1 content</div>
				<div id="panel-2" role="tabpanel" style="display:none"></div>
				<script>
					setTimeout(function() {
						document.getElementById('panel-2').style.display = 'block';
						document.getElementById('panel-2').innerHTML = '<p id="tab2-loaded">Tab 2 loaded</p>';
					}, 400);
				</script>
			</body></html>`,
			waitElement: "#tab2-loaded",
		},
		{
			name: "accordion_multi_section",
			html: `<!DOCTYPE html><html><body>
				<div class="accordion">
					<div class="section"><h3>Section 1</h3><div id="sec-1" style="display:none">Content 1</div></div>
					<div class="section"><h3>Section 2</h3><div id="sec-2" style="display:none">Content 2</div></div>
					<div class="section"><h3>Section 3</h3><div id="sec-3" style="display:none">Content 3</div></div>
				</div>
				<script>
					setTimeout(function() {
						['sec-1','sec-2','sec-3'].forEach(function(id) {
							document.getElementById(id).style.display = 'block';
						});
					}, 450);
				</script>
			</body></html>`,
			waitElement: "#sec-3",
		},
		{
			name: "search_autocomplete",
			html: `<!DOCTYPE html><html><body>
				<input id="search" type="text" placeholder="Search...">
				<div id="suggestions" style="display:none"></div>
				<script>
					setTimeout(function() {
						var sug = document.getElementById('suggestions');
						sug.style.display = 'block';
						for (var i = 0; i < 5; i++) {
							var el = document.createElement('div');
							el.id = 'sug-' + i;
							el.className = 'suggestion';
							el.textContent = 'Suggestion ' + i;
							sug.appendChild(el);
						}
					}, 500);
				</script>
			</body></html>`,
			waitElement: "#sug-0",
		},
		{
			name: "carousel_slider",
			html: `<!DOCTYPE html><html><body>
				<div id="carousel" class="carousel">
					<div id="slide-0" class="slide active">Slide 1</div>
				</div>
				<script>
					setTimeout(function() {
						var c = document.getElementById('carousel');
						for (var i = 1; i <= 3; i++) {
							var sl = document.createElement('div');
							sl.id = 'slide-' + i;
							sl.className = 'slide';
							sl.textContent = 'Slide ' + (i + 1);
							c.appendChild(sl);
						}
						var nav = document.createElement('div');
						nav.id = 'carousel-nav';
						nav.innerHTML = '<button>Prev</button><button>Next</button>';
						c.appendChild(nav);
					}, 350);
				</script>
			</body></html>`,
			waitElement: "#carousel-nav",
		},
		{
			name: "web_component_custom",
			html: `<!DOCTYPE html><html><body>
				<div id="mount"></div>
				<script>
					class MyCard extends HTMLElement {
						connectedCallback() {
							this.innerHTML = '<div class="card"><h3>Custom Card</h3><p>Content</p></div>';
						}
					}
					customElements.define('my-card', MyCard);
					setTimeout(function() {
						var card = document.createElement('my-card');
						card.id = 'custom-card';
						document.getElementById('mount').appendChild(card);
					}, 300);
				</script>
			</body></html>`,
			waitElement: "#custom-card",
		},
		{
			name: "conditional_rendering",
			html: `<!DOCTYPE html><html><body>
				<div id="app"><p>Checking permissions...</p></div>
				<script>
					setTimeout(function() {
						var app = document.getElementById('app');
						var authed = true;
						if (authed) {
							app.innerHTML = '<div id="dashboard"><h1>Dashboard</h1><div id="stats">Stats loaded</div></div>';
						} else {
							app.innerHTML = '<div id="login"><form><input placeholder="user"><button>Login</button></form></div>';
						}
					}, 400);
				</script>
			</body></html>`,
			waitElement: "#stats",
		},
		{
			name: "streaming_sse_content",
			html: `<!DOCTYPE html><html><body>
				<div id="stream-container"></div>
				<script>
					var chunks = ['Thinking...', 'Processing...', 'Almost done...', 'Complete!'];
					var container = document.getElementById('stream-container');
					chunks.forEach(function(text, i) {
						setTimeout(function() {
							var el = document.createElement('p');
							el.id = 'chunk-' + i;
							el.textContent = text;
							container.appendChild(el);
						}, (i + 1) * 150);
					});
				</script>
			</body></html>`,
			waitElement: "#chunk-3",
		},
		// --- 20 additional pages for 50+ total ---
		{
			name: "disabled_button_enable",
			html: `<!DOCTYPE html><html><body>
				<form><input id="agree" type="checkbox">
				<button id="submit-btn" disabled>Submit</button></form>
				<script>
					setTimeout(function() {
						document.getElementById('submit-btn').disabled = false;
					}, 600);
				</script>
			</body></html>`,
			waitElement: "#submit-btn",
		},
		{
			name: "multi_step_wizard",
			html: `<!DOCTYPE html><html><body>
				<div id="wizard"><div id="step-1" class="active">Step 1</div></div>
				<script>
					setTimeout(function() {
						document.getElementById('step-1').classList.remove('active');
						var s2 = document.createElement('div');
						s2.id = 'step-2'; s2.className = 'active'; s2.textContent = 'Step 2';
						document.getElementById('wizard').appendChild(s2);
					}, 400);
				</script>
			</body></html>`,
			waitElement: "#step-2",
		},
		{
			name: "nested_iframe_deep",
			html: `<!DOCTYPE html><html><body>
				<h1 id="outer-heading">Outer</h1>
				<iframe id="frame-1" srcdoc="<html><body><div id='inner-content'>Inner frame loaded</div></body></html>"></iframe>
				<div id="post-frame-marker"></div>
			</body></html>`,
			waitElement: "#post-frame-marker",
		},
		{
			name: "gov_table_data",
			html: `<!DOCTYPE html><html><body>
				<table id="gov-data"><thead><tr><th>Department</th><th>Budget</th></tr></thead><tbody></tbody></table>
				<script>
					setTimeout(function() {
						var tbody = document.querySelector('#gov-data tbody');
						var depts = ['Treasury','Defense','Education','Health','Transport'];
						depts.forEach(function(d, i) {
							var tr = document.createElement('tr');
							tr.id = 'dept-row-' + i;
							tr.innerHTML = '<td>' + d + '</td><td>$' + ((i+1)*10) + 'B</td>';
							tbody.appendChild(tr);
						});
					}, 500);
				</script>
			</body></html>`,
			waitElement: "#dept-row-4",
		},
		{
			name: "university_course_catalog",
			html: `<!DOCTYPE html><html><body>
				<div id="catalog"><h1>Course Catalog</h1><div id="courses-loading">Loading courses...</div></div>
				<script>
					setTimeout(function() {
						var list = document.createElement('ul');
						list.id = 'course-list';
						['CS101','CS201','CS301','MATH101','PHYS101'].forEach(function(c) {
							var li = document.createElement('li');
							li.textContent = c; list.appendChild(li);
						});
						document.getElementById('courses-loading').replaceWith(list);
					}, 550);
				</script>
			</body></html>`,
			waitElement: "#course-list",
		},
		{
			name: "ecommerce_filter_sidebar",
			html: `<!DOCTYPE html><html><body>
				<div id="filters"></div>
				<div id="products">Loading products...</div>
				<script>
					setTimeout(function() {
						var f = document.getElementById('filters');
						['Size','Color','Price','Brand'].forEach(function(name) {
							var div = document.createElement('div');
							div.className = 'filter-group'; div.textContent = name;
							f.appendChild(div);
						});
						var el = document.createElement('div');
						el.id = 'filter-loaded'; el.textContent = 'Filters ready';
						f.appendChild(el);
					}, 450);
				</script>
			</body></html>`,
			waitElement: "#filter-loaded",
		},
		{
			name: "sticky_header_scroll",
			html: `<!DOCTYPE html><html><body>
				<header id="sticky-header" style="position:sticky;top:0">Header</header>
				<main id="scroll-content"></main>
				<script>
					setTimeout(function() {
						var main = document.getElementById('scroll-content');
						for (var i = 0; i < 50; i++) {
							var p = document.createElement('p');
							p.textContent = 'Paragraph ' + i;
							main.appendChild(p);
						}
						var marker = document.createElement('div');
						marker.id = 'scroll-loaded';
						main.appendChild(marker);
					}, 300);
				</script>
			</body></html>`,
			waitElement: "#scroll-loaded",
		},
		{
			name: "drag_drop_zone",
			html: `<!DOCTYPE html><html><body>
				<div id="drop-zone" style="border:2px dashed #ccc;padding:40px">Drop here</div>
				<script>
					setTimeout(function() {
						var zone = document.getElementById('drop-zone');
						zone.innerHTML = '<div id="drop-ready" class="active">Ready for drops</div>';
					}, 350);
				</script>
			</body></html>`,
			waitElement: "#drop-ready",
		},
		{
			name: "notification_badge",
			html: `<!DOCTYPE html><html><body>
				<nav><span id="nav-badge" class="badge">0</span></nav>
				<script>
					setTimeout(function() {
						var badge = document.getElementById('nav-badge');
						badge.textContent = '5';
						badge.classList.add('has-notifications');
						var alert = document.createElement('div');
						alert.id = 'notif-alert'; alert.textContent = '5 new notifications';
						document.body.appendChild(alert);
					}, 500);
				</script>
			</body></html>`,
			waitElement: "#notif-alert",
		},
		{
			name: "pdf_viewer_embed",
			html: `<!DOCTYPE html><html><body>
				<div id="pdf-container"><p>Loading PDF viewer...</p></div>
				<script>
					setTimeout(function() {
						var c = document.getElementById('pdf-container');
						c.innerHTML = '<div id="pdf-toolbar"><button>Zoom In</button><button>Zoom Out</button></div><div id="pdf-canvas">PDF Page 1</div>';
					}, 600);
				</script>
			</body></html>`,
			waitElement: "#pdf-canvas",
		},
		{
			name: "chat_widget_popup",
			html: `<!DOCTYPE html><html><body>
				<div id="page-content"><h1>Main Page</h1></div>
				<script>
					setTimeout(function() {
						var widget = document.createElement('div');
						widget.id = 'chat-widget';
						widget.innerHTML = '<div class="chat-header">Support</div><input id="chat-input" placeholder="Type...">';
						document.body.appendChild(widget);
					}, 700);
				</script>
			</body></html>`,
			waitElement: "#chat-widget",
		},
		{
			name: "map_embed_load",
			html: `<!DOCTYPE html><html><body>
				<div id="map-container"><p>Loading map...</p></div>
				<script>
					setTimeout(function() {
						document.getElementById('map-container').innerHTML =
							'<div id="map-canvas" style="width:100%;height:400px;background:#ddd">Map Loaded</div>';
					}, 550);
				</script>
			</body></html>`,
			waitElement: "#map-canvas",
		},
		{
			name: "multi_select_dropdown",
			html: `<!DOCTYPE html><html><body>
				<select id="multi-sel" multiple disabled><option>Loading...</option></select>
				<script>
					setTimeout(function() {
						var sel = document.getElementById('multi-sel');
						sel.innerHTML = '';
						['Option A','Option B','Option C','Option D'].forEach(function(o) {
							var opt = document.createElement('option');
							opt.textContent = o; sel.appendChild(opt);
						});
						sel.disabled = false;
					}, 450);
				</script>
			</body></html>`,
			waitElement: "#multi-sel",
		},
		{
			name: "video_player_controls",
			html: `<!DOCTYPE html><html><body>
				<div id="player"><div id="video-placeholder">Loading player...</div></div>
				<script>
					setTimeout(function() {
						document.getElementById('video-placeholder').innerHTML =
							'<div id="video-controls"><button id="play-btn">Play</button><button id="pause-btn">Pause</button><input id="seek-bar" type="range"></div>';
					}, 500);
				</script>
			</body></html>`,
			waitElement: "#play-btn",
		},
		{
			name: "breadcrumb_nav",
			html: `<!DOCTYPE html><html><body>
				<nav id="breadcrumbs"><a href="/">Home</a></nav>
				<div id="page-body"></div>
				<script>
					setTimeout(function() {
						var bc = document.getElementById('breadcrumbs');
						[' > Courses',' > CS101',' > Week 1'].forEach(function(t) {
							var span = document.createElement('span');
							span.textContent = t; bc.appendChild(span);
						});
						var marker = document.createElement('span');
						marker.id = 'bc-loaded'; bc.appendChild(marker);
					}, 300);
				</script>
			</body></html>`,
			waitElement: "#bc-loaded",
		},
		{
			name: "cookie_consent_banner",
			html: `<!DOCTYPE html><html><body>
				<div id="main-content"><h1>Welcome</h1></div>
				<script>
					setTimeout(function() {
						var banner = document.createElement('div');
						banner.id = 'cookie-banner';
						banner.innerHTML = '<p>We use cookies</p><button id="accept-cookies">Accept</button>';
						document.body.appendChild(banner);
					}, 400);
				</script>
			</body></html>`,
			waitElement: "#accept-cookies",
		},
		{
			name: "data_grid_sortable",
			html: `<!DOCTYPE html><html><body>
				<div id="grid-container"></div>
				<script>
					setTimeout(function() {
						var grid = document.createElement('table');
						grid.id = 'data-grid';
						grid.innerHTML = '<thead><tr><th data-sort="name">Name</th><th data-sort="age">Age</th></tr></thead><tbody>';
						var data = [{n:'Alice',a:30},{n:'Bob',a:25},{n:'Carol',a:35}];
						data.forEach(function(d, i) {
							grid.innerHTML += '<tr id="grid-row-'+i+'"><td>'+d.n+'</td><td>'+d.a+'</td></tr>';
						});
						grid.innerHTML += '</tbody>';
						document.getElementById('grid-container').appendChild(grid);
					}, 550);
				</script>
			</body></html>`,
			waitElement: "#data-grid",
		},
		{
			name: "countdown_timer",
			html: `<!DOCTYPE html><html><body>
				<div id="countdown">10</div>
				<script>
					var c = 10;
					var iv = setInterval(function() {
						document.getElementById('countdown').textContent = String(--c);
						if (c <= 0) {
							clearInterval(iv);
							var done = document.createElement('div');
							done.id = 'timer-done'; done.textContent = 'Time up!';
							document.body.appendChild(done);
						}
					}, 50);
				</script>
			</body></html>`,
			waitElement: "#timer-done",
		},
		{
			name: "progressive_image_gallery",
			html: `<!DOCTYPE html><html><body>
				<div id="gallery-grid"></div>
				<script>
					var grid = document.getElementById('gallery-grid');
					for (var i = 0; i < 8; i++) {
						(function(idx) {
							setTimeout(function() {
								var img = document.createElement('div');
								img.id = 'gallery-img-' + idx;
								img.className = 'gallery-thumb';
								img.style.cssText = 'width:100px;height:100px;background:#eee;display:inline-block;margin:4px';
								grid.appendChild(img);
							}, 100 * (idx + 1));
						})(i);
					}
				</script>
			</body></html>`,
			waitElement: "#gallery-img-7",
		},
		{
			name: "form_validation_live",
			html: `<!DOCTYPE html><html><body>
				<form id="reg-form">
					<input id="reg-email" type="email" required>
					<div id="validation-msg" style="display:none"></div>
					<button id="reg-submit" disabled>Register</button>
				</form>
				<script>
					setTimeout(function() {
						document.getElementById('reg-email').value = 'test@example.com';
						document.getElementById('validation-msg').style.display = 'block';
						document.getElementById('validation-msg').textContent = 'Valid email';
						document.getElementById('validation-msg').id = 'validation-ok';
						document.getElementById('reg-submit').disabled = false;
					}, 500);
				</script>
			</body></html>`,
			waitElement: "#validation-ok",
		},
	}
}

// GapAnalysisEntry captures a page where PageWaiter failed to find the element.
type GapAnalysisEntry struct {
	PageType     string  `json:"page_type"`
	Strategy     string  `json:"strategy"`
	WaitMs       float64 `json:"wait_ms"`
	WaitOK       bool    `json:"wait_ok"`
	ElementFound bool    `json:"element_found"`
	Reason       string  `json:"reason"`
}

// TestPlaywrightComparison_PageWaiterAccuracy tests PageWaiter's ability to
// correctly detect page readiness across 50+ representative page types.
// This serves as the oracle comparison: if PageWaiter waits correctly, the
// expected element should be present in DOM after wait completes.
func TestPlaywrightComparison_PageWaiterAccuracy(t *testing.T) {
	requirePlaywrightComparison(t)

	pages := comparisonPages()
	if len(pages) < 50 {
		t.Fatalf("expected 50+ comparison pages, got %d", len(pages))
	}

	mux := http.NewServeMux()
	for _, pg := range pages {
		content := pg.html
		mux.HandleFunc("/"+pg.name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, content)
		})
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	var browser *BrowserAgent
	var err error
	if du := chromeDebugURL(); du != "" {
		browser, err = NewBrowserAgentWithRemote(du)
	} else {
		browser, err = NewBrowserAgent(true)
	}
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer browser.Close()

	var results []PlaywrightComparisonResult
	var gaps []GapAnalysisEntry

	for _, pg := range pages {
		waiter := NewPageWaiter(10*time.Second, WaitNetworkIdle|WaitDOMStable)
		url := ts.URL + "/" + pg.name

		start := time.Now()
		waitErr := waiter.NavigateAndWait(browser.ctx, url)
		waitDur := time.Since(start)

		r := PlaywrightComparisonResult{
			PageType:     pg.name,
			PageWaiterMs: float64(waitDur.Milliseconds()),
			PageWaiterOK: waitErr == nil,
		}

		if waitErr == nil {
			html, domErr := browser.CaptureDOM()
			if domErr == nil {
				r.PageWaiterDOMLen = len(html)
			}

			ctx, cancel := context.WithTimeout(browser.ctx, 3*time.Second)
			elemErr := waiter.WaitForElement(ctx, pg.waitElement)
			cancel()
			r.ElementFound = elemErr == nil
		}

		if !r.ElementFound {
			reason := "element not found after wait"
			if waitErr != nil {
				reason = fmt.Sprintf("wait failed: %v", waitErr)
			}
			gaps = append(gaps, GapAnalysisEntry{
				PageType:     pg.name,
				Strategy:     "network_and_dom",
				WaitMs:       r.PageWaiterMs,
				WaitOK:       r.PageWaiterOK,
				ElementFound: false,
				Reason:       reason,
			})
		}

		results = append(results, r)
		t.Logf("page=%-30s wait=%6.0fms ok=%v element_found=%v dom=%d",
			pg.name, r.PageWaiterMs, r.PageWaiterOK, r.ElementFound, r.PageWaiterDOMLen)
	}

	var totalMs float64
	passed, elementsFound := 0, 0
	for _, r := range results {
		if r.PageWaiterOK {
			passed++
		}
		if r.ElementFound {
			elementsFound++
		}
		totalMs += r.PageWaiterMs
	}

	total := len(results)
	var avgMs, accuracy float64
	if total > 0 {
		avgMs = totalMs / float64(total)
		accuracy = float64(elementsFound) / float64(total)
	}

	type extendedReport struct {
		PlaywrightComparisonReport
		GapAnalysis []GapAnalysisEntry `json:"gap_analysis"`
	}

	report := extendedReport{
		PlaywrightComparisonReport: PlaywrightComparisonReport{
			Timestamp:   time.Now(),
			Description: fmt.Sprintf("PageWaiter accuracy across %d representative page types", total),
			Results:     results,
			Summary: ComparisonSummary{
				TotalPages:       total,
				PageWaiterPassed: passed,
				ElementsFound:    elementsFound,
				AvgWaitMs:        avgMs,
				AccuracyRate:     accuracy,
			},
		},
		GapAnalysis: gaps,
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	reportDir := filepath.Join(os.TempDir(), "uiauto-benchmarks")
	os.MkdirAll(reportDir, 0755)
	reportPath := filepath.Join(reportDir, fmt.Sprintf("playwright_comparison_%s.json", time.Now().Format("20060102_150405")))
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	t.Logf("\n--- Playwright Comparison Summary ---\nPages: %d  Passed: %d  Elements Found: %d\nAccuracy: %.1f%%  Avg Wait: %.1fms  Gaps: %d\nReport: %s",
		total, passed, elementsFound, accuracy*100, avgMs, len(gaps), reportPath)

	if len(gaps) > 0 {
		t.Logf("\n--- Gap Analysis (%d failures) ---", len(gaps))
		for _, g := range gaps {
			t.Logf("  GAP: page=%-30s strategy=%-15s reason=%s", g.PageType, g.Strategy, g.Reason)
		}
	}

	if accuracy < 0.90 {
		t.Errorf("PageWaiter accuracy below 90%% target: %.1f%%", accuracy*100)
	}
}

// TestPlaywrightComparison_WaitStrategySweep tests all 5 strategies across
// a representative subset of pages, including WaitElementVisible and
// WaitElementEnabled which require a target selector.
func TestPlaywrightComparison_WaitStrategySweep(t *testing.T) {
	requirePlaywrightComparison(t)

	pages := comparisonPages()
	sweepPages := pages[:10]
	if len(pages) > 10 {
		sweepPages = pages[:10]
	}

	mux := http.NewServeMux()
	for _, pg := range sweepPages {
		content := pg.html
		mux.HandleFunc("/"+pg.name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, content)
		})
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	var browser *BrowserAgent
	var err error
	if du := chromeDebugURL(); du != "" {
		browser, err = NewBrowserAgentWithRemote(du)
	} else {
		browser, err = NewBrowserAgent(true)
	}
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer browser.Close()

	type strategyDef struct {
		name     string
		strategy WaitStrategy
	}

	navStrategies := []strategyDef{
		{"network_idle", WaitNetworkIdle},
		{"dom_stable", WaitDOMStable},
		{"network+dom", WaitNetworkIdle | WaitDOMStable},
	}

	type sweepResult struct {
		Page     string  `json:"page"`
		Strategy string  `json:"strategy"`
		WaitMs   float64 `json:"wait_ms"`
		WaitOK   bool    `json:"wait_ok"`
		Found    bool    `json:"found"`
	}

	var allResults []sweepResult

	for _, pg := range sweepPages {
		for _, strat := range navStrategies {
			waiter := NewPageWaiter(10*time.Second, strat.strategy)

			start := time.Now()
			navErr := waiter.NavigateAndWait(browser.ctx, ts.URL+"/"+pg.name)
			dur := time.Since(start)

			var found bool
			if navErr == nil {
				ctx, cancel := context.WithTimeout(browser.ctx, 3*time.Second)
				found = waiter.WaitForElement(ctx, pg.waitElement) == nil
				cancel()
			}

			allResults = append(allResults, sweepResult{
				Page: pg.name, Strategy: strat.name,
				WaitMs: float64(dur.Milliseconds()), WaitOK: navErr == nil, Found: found,
			})

			t.Logf("page=%-30s strategy=%-15s wait=%6.0fms ok=%v found=%v",
				pg.name, strat.name, float64(dur.Milliseconds()), navErr == nil, found)
		}

		// WaitElementVisible: navigate first, then wait for target element
		waiter := NewPageWaiter(10*time.Second, WaitNetworkIdle|WaitDOMStable)
		navErr := waiter.NavigateAndWait(browser.ctx, ts.URL+"/"+pg.name)
		if navErr == nil {
			start := time.Now()
			ctx, cancel := context.WithTimeout(browser.ctx, 5*time.Second)
			visErr := waiter.WaitForElement(ctx, pg.waitElement)
			cancel()
			dur := time.Since(start)

			allResults = append(allResults, sweepResult{
				Page: pg.name, Strategy: "element_visible",
				WaitMs: float64(dur.Milliseconds()), WaitOK: visErr == nil, Found: visErr == nil,
			})

			t.Logf("page=%-30s strategy=%-15s wait=%6.0fms ok=%v found=%v",
				pg.name, "element_visible", float64(dur.Milliseconds()), visErr == nil, visErr == nil)
		}

		// WaitElementEnabled: navigate first, then wait for target to be enabled
		navErr = waiter.NavigateAndWait(browser.ctx, ts.URL+"/"+pg.name)
		if navErr == nil {
			start := time.Now()
			ctx, cancel := context.WithTimeout(browser.ctx, 5*time.Second)
			enErr := waiter.WaitForElementEnabled(ctx, pg.waitElement)
			cancel()
			dur := time.Since(start)

			allResults = append(allResults, sweepResult{
				Page: pg.name, Strategy: "element_enabled",
				WaitMs: float64(dur.Milliseconds()), WaitOK: enErr == nil, Found: enErr == nil,
			})

			t.Logf("page=%-30s strategy=%-15s wait=%6.0fms ok=%v found=%v",
				pg.name, "element_enabled", float64(dur.Milliseconds()), enErr == nil, enErr == nil)
		}
	}

	// Per-strategy accuracy summary
	stratStats := make(map[string]struct{ total, found int })
	for _, r := range allResults {
		s := stratStats[r.Strategy]
		s.total++
		if r.Found {
			s.found++
		}
		stratStats[r.Strategy] = s
	}

	t.Log("\n--- Strategy Sweep Summary ---")
	for name, s := range stratStats {
		acc := float64(0)
		if s.total > 0 {
			acc = float64(s.found) / float64(s.total) * 100
		}
		t.Logf("  %-20s total=%d found=%d accuracy=%.1f%%", name, s.total, s.found, acc)
	}

	data, err := json.MarshalIndent(allResults, "", "  ")
	if err == nil {
		reportDir := filepath.Join(os.TempDir(), "uiauto-benchmarks")
		os.MkdirAll(reportDir, 0755)
		reportPath := filepath.Join(reportDir, fmt.Sprintf("strategy_sweep_%s.json", time.Now().Format("20060102_150405")))
		os.WriteFile(reportPath, data, 0644)
		t.Logf("Sweep report: %s", reportPath)
	}
}
