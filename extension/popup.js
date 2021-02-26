let {downloads, states} = chrome.extension.getBackgroundPage()

downloads.switchUpdates(true)
window.addEventListener('close', () => downloads.switchUpdates(false))

// ================ URL ======================

const urlInput = document.getElementById('url')
const toolbar = document.getElementsByTagName('ui-toolbar')[0]


document.getElementById('add').addEventListener('click', () => {
    toolbar.style.display = 'none'
    urlInput.parentElement.style.display = 'flex'
    urlInput.value = ''
    urlInput.focus()
})

function commitUrl() {
    let url = urlInput.value.trim()
    if (url.length == 0) {
        return
    }
    try {
        new URL(url)
    } catch {
        return
    }
    chrome.downloads.download({ url: urlInput.value })
    resetUrl()
}

function resetUrl() {
    urlInput.parentElement.style.display = 'none'
    toolbar.style.display = 'flex'
}

urlInput.addEventListener('keypress', event => {
    if (event.key == 'Enter') {
        commitUrl()
    }
})

document.getElementById('add-url').addEventListener('click', commitUrl)
document.getElementById('cancel-url').addEventListener('click', resetUrl)

// ================ LIST-ITEMS ======================

let lastFocusItem

const partsNames = ['progress', 'percent', 'speed', 'eta', 'conns']

const progressBarColors = {
    [states.downloading]: 'cyan',
    [states.failed]: 'red',
    [states.paused]: 'grey',
    [states.rebuilding]: 'lightgreen',
    [states.urlPending]: 'orange',
}

const staticMsgs = {
    [states.paused]: 'Paused',
    [states.failed]: 'Failed',
    [states.rebuilding]: 'Rebuilding...',
    [states.urlPending]: 'Waiting for new URL...',
}

const buttonsOnItems = {
    resume: document.getElementById('resume'),
    pause: document.getElementById("pause"),
    copyUrl: document.getElementById("copy-url"),
    changeUrl: document.getElementById('change-url'),
    open: document.getElementById('open'),
    folder: document.getElementById('folder'),
    remove: document.getElementById('remove'),
}

const buttonsByState = {
    [states.downloading]: ['pause', 'copyUrl'],
    [states.failed]: ['resume', 'copyUrl', 'changeUrl', 'remove'],
    [states.paused]: ['resume', 'copyUrl', 'changeUrl', 'remove'],
    [states.rebuilding]: ['copyUrl'],
    [states.urlPending]: ['resume', 'copyUrl', 'remove'],
    [states.completed]: ['copyUrl', 'open', 'folder', 'remove'],
}

customElements.define('download-item', class extends HTMLElement {

    constructor(...args) {
        super(...args)

        this.addEventListener('focus', () => {
            if (lastFocusItem) {
                lastFocusItem.removeAttribute('focused')
            }
            this.setAttribute('focused', true)  // for styling
            lastFocusItem = this
            this.updateButtons()
        })
    }

    updateButtons() {
        for (let [id, butt] of Object.entries(buttonsOnItems)) {
            if (buttonsByState[this.data.state].includes(id)) {
                butt.style.display = 'inline'
            } else {
                butt.style.display = 'none'
            }
        }
    }

    async connectedCallback() {
        this.setAttribute('tabindex', 0)
        this.data = downloads.items[this.id]

        this.icon = document.createElement('img')
        this.icon.src = downloads.icons[this.data.icon]?.url
        this.appendChild(this.icon)

        this.fname = document.createElement("ui-name")
        this.fname.innerText = this.data.filename
        this.appendChild(this.fname)

        this.size = document.createElement("ui-size")
        this.size.innerText = this.data.size
        this.appendChild(this.size)

        this.date = document.createElement("ui-date")
        this.date.innerText = new Date(this.data.date).toLocaleDateString()
        this.appendChild(this.date)

        if (this.data.state == states.completed) {
            return
        }
        for (let name of partsNames) {
            this[name] = document.createElement('ui-' + name)
        }
        this.update()
    }

    async update() {
        if (this.id == lastFocusItem?.id) {
            this.updateButtons()
        }
        if (this.data.state == states.completed) {
            this.size.innerText = this.data.size
            this.fname.innerText = this.data.filename
            for (let e of partsNames) {
                this[e].remove()
                delete this[e]
            }
            return
        }
        // not completed
        this.size.innerText = this.data.written + ' / ' + this.data.size
        let lastPartI = this.data.state == states.downloading ? partsNames.length : 1
        let percent = (Math.round(this.data.percent * 100) / 100) + '%'
        for (let name of partsNames.slice(0, lastPartI)) {
            let elm = this[name]
            switch (name) {
                case 'progress':
                    elm.style.width = percent
                    elm.style.background = progressBarColors[this.data.state]
                    break
                case 'percent':
                    elm.innerText = percent
                    break
                case 'conns':
                    elm.innerText = 'x' + this.data.conns
                    break
                default:
                    elm.innerText = this.data[name]
            }
            if (elm.parentElement == null) {
                this.appendChild(elm)
            }
        }
        if (this.data.state != states.downloading) {
            let msgElm = this[partsNames[lastPartI]]
            msgElm.innerText = staticMsgs[this.data.state]
            this.appendChild(msgElm)
            lastPartI++
        }
        for (let name of partsNames.slice(lastPartI)) {
            this[name].remove()
        }
    }
})

let list = document.getElementsByTagName('ui-list')[0];
let inProgressItems = {};  // elements by id, to update without using document.getElementById which may use more resources
(() => {
    // populate the list
    let items = document.createDocumentFragment()  // to reduce redrawing
    for (let [id, data] of Object.entries(downloads.items)
        .sort((a, b) => a[1].date < b[1].date ? 1 : -1)  // by date, most recent
        .sort((a, b) => a[1].state < b[1].state ? -1 : 1)) {  // by status, in progress at the top
        let item = document.createElement('download-item')
        item.id = id
        items.appendChild(item)
        if (data.state != states.completed) {
            inProgressItems[id] = item
        }
    }
    list.appendChild(items)
})()

function add(id, data) {
    let item = document.createElement('download-item')
    item.id = id
    list.insertAdjacentElement('afterbegin', item)
    if (data.state != states.completed) {
        inProgressItems[id] = item
    }
}

function update(id) {
    inProgressItems[id].update()
    if (downloads.items[id].state == states.completed) {
        delete inProgressItems[id]
    }
}

// ===================== LIST ======================

document.getElementById('remove').addEventListener('click', event => {
    event.preventDefault()
    if (lastFocusItem == null) return
    if (downloads.remove(Number(lastFocusItem.id))) {
        lastFocusItem.remove()
        lastFocusItem = undefined
        // update buttons
        for (let butt of Object.values(buttonsOnItems)) {
            butt.style.display = 'none'
        }
    }
})

function pauseResume(event) {
    event.preventDefault()
    if (lastFocusItem == null) return
    let stateTo = event.target.id == 'pause' ? states.paused : states.downloading
    downloads.changeState(Number(lastFocusItem.id), stateTo)
}
document.getElementById('pause').addEventListener('click', pauseResume)
document.getElementById('pause-all').addEventListener('click', () => {
    event.preventDefault()
    downloads.pauseAll()
})
document.getElementById('resume').addEventListener('click', pauseResume)

document.getElementById('clear').addEventListener('click', event => {
    event.preventDefault()
    for (let id of Object.keys(downloads.items)) {
        if (downloads.remove(Number(id))) {
            document.getElementById(id)?.remove()
        }
    }
    if (downloads.items[lastFocusItem.id]) {
        return
    }
    // focused item also cleared, update buttons
    for (let butt of Object.values(buttonsOnItems)) {
        butt.style.display = 'none'
    }
})

function openPath(event) {
    event.preventDefault()
    if (lastFocusItem == null) return
    if (event.target.id == 'open') {
        downloads.openFile(Number(lastFocusItem.id))
    } else {
        downloads.openDir(Number(lastFocusItem.id))
    }
}

document.getElementById('open').addEventListener('click', openPath)
document.getElementById('folder').addEventListener('click', openPath)

document.getElementById('change-url').addEventListener('click', event => {
    event.preventDefault()
    if (lastFocusItem == null) return
    let down = downloads.items[lastFocusItem.id]
    if (downloads.urlPending) {
        let lastWaiting = downloads[downloads.urlPending].state
        if (lastWaiting.error) {
            lastWaiting.state = states.failed
        } else {
            lastWaiting.state = states.paused
        }
    }
    downloads.urlPending = Number(lastFocusItem.id)
    if ([states.failed, states.paused, states.urlPending].includes(downloads.items[lastFocusItem.id].state)) {
        down.state = states.urlPending
        update(Number(lastFocusItem.id))
    }
})

document.getElementById('copy-url').addEventListener('click', () => {
    event.preventDefault()
    if (lastFocusItem == undefined) {
        return
    }
    navigator.clipboard.writeText(downloads.items[lastFocusItem.id]?.url)
})

// ==================== SETTINGS ===================

let settingsElements
let headerTitle = document.getElementById('title').innerText

document.getElementById('settings').addEventListener('change', event => {
    let header = document.getElementById('title')
    if (!event.target.checked) {
        header.innerText = headerTitle
        return
    }
    header.innerText = 'Settings'
    // update settings
    if (settingsElements == undefined) {
        settingsElements = {
            categories: document.getElementById('categories'),
            conns: document.getElementById('conns'),
            notify: {
                begin: document.getElementById('notify-begin'),
                end: document.getElementById('notify-end'),
            },
        }
    }
    retrieveSettings()
})

function parseCats(cats) {
    let categories = {}
    for (let line of cats.split('\n')) {
        line = line.trim()
        if (line == '') {
            continue
        }
        let [name, extensions] = line.split(':')
        if (extensions == undefined) {
            continue
        }
        name = name.trim()
        extensions = extensions.trim()
        if (name == '' || extensions == '') {
            continue
        }
        let exts = []
        for (let ext of extensions.split(' ')) {
            if (ext == '') {
                continue
            }
            exts.push(ext)
        }
        if (exts.length == 0) {
            continue
        }
        categories[name] = exts
    }
    return categories
}

function retrieveSettings() {
    settingsElements.conns.value = downloads.settings.conns
    let cats = []
    for (let [name, exts] of Object.entries(downloads.settings.categories)) {
        cats.push(name + ': ' + exts.join(' '))
    }
    settingsElements.categories.value = cats.join('\n')
    settingsElements.notify.begin.checked = downloads.settings.notify.begin
    settingsElements.notify.end.checked = downloads.settings.notify.end
}

document.getElementById('save-settings').addEventListener('click', event => {
    event.preventDefault()
    let settings = {
        conns: Number(settingsElements.conns.value),
        categories: parseCats(settingsElements.categories.value),
        notify: {
            begin: settingsElements.notify.begin.checked,
            end: settingsElements.notify.end.checked,
        }
    }
    chrome.storage.local.set({ settings }, () => {
        downloads.settings = settings
        retrieveSettings()
        let prevText = event.target.innerText
        event.target.innerText = 'Saved'
        setTimeout(() => {
            event.target.innerText = prevText
        }, 1000)
    })
})

document.getElementById('reset-settings').addEventListener('click', event => {
    event.preventDefault()
    chrome.storage.local.set({ settings: downloads.settingsDefault }, () => {
        downloads.settings = downloads.settingsDefault
        retrieveSettings()
    })
})

// open project source
document.getElementById('source').addEventListener('click', event => {
    event.preventDefault()
    chrome.tabs.create({url: event.target.href})
    window.close()
})
