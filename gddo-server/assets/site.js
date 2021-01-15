// modal
function Modal(el) {
	if (el == null) {
		return null
	}

	this.el = el
	el.querySelector(".close").onclick = () => this.hide()
	el.onclick = () => this.hide()
	el.onkeydown = (e) => {
		if (e.key == "Escape") {
			this.hide()
		}
	}
	el.querySelector(".modal-dialog").onclick = function(e) {
		e.stopPropagation()
	}
}

Modal.prototype.show = function() {
	this.el.classList.add("show")
	this.el.focus()
}

Modal.prototype.hide = function() {
	this.el.classList.remove("show")
}

// jump modal
function JumpModal(el) {
	if (el == null) {
		return null
	}

	this.all = []
	this.visible = []
	this.active = -1
	this.lastFilter = ""
	this.modal = new Modal(el)
	this.body = el.querySelector("#x-jump-body")
	this.list = el.querySelector("#x-jump-list")
	this.filter = el.querySelector("#x-jump-filter")

	this.filter.oninput = e => {
		var filter = e.target.value
		if (filter.toLowerCase() != this.lastFilter.toLowerCase()) {
			this.update(filter)
		}
	}

	this.filter.onkeydown = e => {
		switch(e.key) {
		case "ArrowUp":
			this.incrActive(-1)
			e.preventDefault()
			break
		case "ArrowDown":
			this.incrActive(1)
			e.preventDefault()
			break
		case "Enter":
			if (this.active >= 0) {
				this.visible[this.active].el.click()
			}
			break
		}
	}
}

JumpModal.prototype.update = function(filter) {
	this.lastFilter = filter
	if (this.active >= 0) {
		this.visible[this.active].el.classList.remove("active")
		this.active = -1
	}

	// Update visible elements
	this.visible = []
	var re = new RegExp(filter.replace(/([.*+?^=!:${}()|\[\]\/\\])/g, "\\$1"), "gi")
	this.visible = this.all.filter((id) => {
		// Detatch element
		if (id.el.parentElement != null) {
			id.el.parentElement.removeChild(id.el)
		}

		var text = id.text
		if (filter.length > 0) {
			text = id.text.replace(re, function(s) {
				return "<b>" + s + "</b>"
			})
			if (text == id.text) {
				return false
			}
		}

		id.el.innerHTML = text + " " + "<i>" + id.kind + "</i>"
		return true
	})

	this.body.scrollTop = 0
	if (this.visible.length > 0) {
		this.active = 0
		this.visible[this.active].el.classList.add("active")
	}

	for (var i = 0; i < this.visible.length; i++) {
		this.list.appendChild(this.visible[i].el)
	}
}

JumpModal.prototype.incrActive = function(delta) {
	if (this.visible.length == 0) {
		return
	}

	this.visible[this.active].el.classList.remove("active")

	this.active += delta
	if (this.active < 0) {
		this.active = 0
	} else if (this.active >= this.visible.length) {
		this.active = this.visible.length - 1
	}

	var el = this.visible[this.active].el
	el.scrollIntoView({
		block: "nearest",
	})
	el.classList.add("active")
}

JumpModal.prototype.show = function() {
	if (this.all.length == 0) {
		document.querySelectorAll("[id]").forEach(e => {
			var id = e.id
			if (/^[^_][^-]*$/.test(id)) {
				var el = document.createElement("a")
				el.href = "#" + id
				el.classList.add("list-group-item")
				el.tabindex = -1
				el.onclick = () => {
					this.modal.hide()
				}

				this.all.push({
					text: id,
					ltext: id.toLowerCase(),
					kind: e.closest("[data-kind]").dataset.kind,
					el: el,
				})
			}
		})

		this.all.sort(function(a, b) {
			if (a.ltext > b.ltext) {
				return 1
			}
			if (a.ltext < b.ltext) {
				return -1
			}
			return 0
		})
	}

	this.update("")
	this.modal.show()
	this.filter.value = ""
	this.filter.focus()
}

// navbar toggle
var navToggle = document.querySelector(".navbar-toggle")
navToggle.onclick = function() {
	document.querySelector(".navbar-collapse").classList.toggle("show")
}

// keyboard shortcuts
var search = document.querySelector("#x-search-query")
var shortcuts = new Modal(document.querySelector("#x-shortcuts"))
var jump = new JumpModal(document.querySelector("#x-jump"))
var prevCh = null
var prevTime = 0

document.onkeydown = function(e) {
	var combo = e.timeStamp - prevTime <= 1000
	prevTime = 0

	if (e.target != document.body) {
		return
	}
	if (e.metaKey || e.ctrlKey) {
		return true
	}

	var ch = e.key
	if (combo) {
		switch (prevCh + ch) {
		case "gg":
			window.scrollTo(0, 0)
			return false
		case "gb":
			window.scrollTo(0, document.body.scrollHeight)
			return false
		case "gi":
			var pkgIndex = document.querySelector("#pkg-index")
			if (pkgIndex != null) {
				pkgIndex.scrollIntoView()
				return false
			}
		case "ge":
			var pkgExamples = document.querySelector("#pkg-examples")
			if (pkgExamples != null && pkgExamples.children.length > 0) {
				pkgExamples.scrollIntoView()
				return false
			}
		}
	}

	switch (ch) {
	case "/":
		if (search != null) {
			search.focus()
		}
		return false
	case "?":
		if (shortcuts != null) {
			shortcuts.show()
		}
		return false
	case "G":
		window.scrollTo(0, document.body.scrollHeight)
		return false
	case "f":
		if (jump != null) {
			jump.show()
			return false
		}
	}

	prevCh = ch
	prevTime = e.timeStamp
	return true
}

function onhashchange() {
	// open selected example
	var hash = window.location.hash
	if (hash.startsWith("#example-")) {
		document.querySelector(hash).parentElement.setAttribute("open", "")
	}
}
window.addEventListener("hashchange", onhashchange)
onhashchange()
