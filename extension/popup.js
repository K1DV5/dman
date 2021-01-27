let bg = chrome.extension.getBackgroundPage()

bg.switchUpdates(true)
window.addEventListener('close', () => bg.switchUpdates(false))

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
    } else if (event.key == 'Esc') {
        event.preventDefault()
        resetUrl()
    }
})
document.getElementById('add-url').addEventListener('click', commitUrl)
document.getElementById('cancel-url').addEventListener('click', resetUrl)

// ================ LIST-ITEMS ======================

let lastFocusItem

let partsNames = ['progress', 'percent', 'speed', 'eta', 'conns']

let progressBarColors = {
    [bg.S_DOWNLOADING]: 'cyan',
    [bg.S_FAILED]: 'red',
    [bg.S_PAUSED]: 'grey',
    [bg.S_REBUILDING]: 'lightgreen',
    [bg.S_WAIT_URL]: 'orange',
}

let staticMsgs = {
    [bg.S_PAUSED]: 'Paused',
    [bg.S_FAILED]: 'Failed',
    [bg.S_REBUILDING]: 'Rebuilding...',
    [bg.S_WAIT_URL]: 'Waiting for new URL...',
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
        })
    }

    async connectedCallback() {
        this.setAttribute('tabindex', 0)
        this.data = bg.downloads[this.id]

        this.icon = document.createElement('img')
        this.icon.src = bg.icons[this.data.icon]?.url
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

        if (this.data.state == bg.S_COMPLETED) {
            return
        }
        for (let name of partsNames) {
            this[name] = document.createElement('ui-' + name)
        }
        this.update()
    }

    async update() {
        if (this.data.state == bg.S_COMPLETED) {
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
        let lastPartI = this.data.state == bg.S_DOWNLOADING ? partsNames.length : 1
        for (let name of partsNames.slice(0, lastPartI)) {
            let elm = this[name]
            let percent = (Math.round(this.data.percent * 100) / 100) + '%'
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
        if (this.data.state != bg.S_DOWNLOADING) {
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
    for (let [id, data] of Object.entries(bg.downloads)
        .sort((a, b) => a[1].date < b[1].date ? 1 : -1)  // by date, most recent
        .sort((a, b) => a[1].state < b[1].state ? -1 : 1)) {  // by status, in progress at the top
        let item = document.createElement('download-item')
        item.id = id
        items.appendChild(item)
        if (data.state != bg.S_COMPLETED) {
            inProgressItems[id] = item
        }
    }
    list.appendChild(items)
})()

function add(id, data) {
    let item = document.createElement('download-item')
    item.id = id
    list.insertAdjacentElement('afterbegin', item)
    if (data.state != bg.S_COMPLETED) {
        inProgressItems[id] = item
    }
}

function update(id) {
    inProgressItems[id].update()
    if (bg.downloads[id].state == bg.S_COMPLETED) {
        delete inProgressItems[id]
    }
}

// ===================== LIST ======================

function finishRemove(id) {
    let item = document.getElementById(id)
    if (item == null) return
    item.remove()
}

document.getElementById('remove').addEventListener('click', event => {
    event.preventDefault()
    if (lastFocusItem == null) return
    if (bg.removeItem(lastFocusItem.id)) {
        lastFocusItem = undefined
    }
})

function pauseResume(event) {
    event.preventDefault()
    if (lastFocusItem == null) return
    let stateTo = event.target.id == 'pause' ? bg.S_PAUSED : bg.S_DOWNLOADING
    bg.changeState(Number(lastFocusItem.id), stateTo)
}
document.getElementById('pause').addEventListener('click', pauseResume)
document.getElementById('pause-all').addEventListener('click', () => {
    event.preventDefault()
    bg.pauseAll()
})
document.getElementById('resume').addEventListener('click', pauseResume)

document.getElementById('clear').addEventListener('click', event => {
    event.preventDefault()
    for (let [id, down] of Object.entries(bg.downloads)) {
        // clear only the completed, paused and failed ones
        if (!(down.state == bg.S_DOWNLOADING || down.state == bg.S_REBUILDING)) {
            bg.removeItem(Number(id))
        }
    }
})

function openPath(event) {
    event.preventDefault()
    if (lastFocusItem == null) return
    if (event.target.id == 'open') {
        bg.openFile(Number(lastFocusItem.id))
    } else {
        bg.openDir(Number(lastFocusItem.id))
    }
}

document.getElementById('open').addEventListener('click', openPath)
document.getElementById('folder').addEventListener('click', openPath)

document.getElementById('change-url').addEventListener('click', event => {
    event.preventDefault()
    if (lastFocusItem == null) return
    let down = bg.downloads[lastFocusItem.id]
    if (bg.waitingUrl) {
        let lastWaiting = bg.downloads[bg.waitingUrl].state
        if (lastWaiting.error) {
            lastWaiting.state = bg.S_FAILED
        } else {
            lastWaiting.state = bg.S_PAUSED
        }
    }
    bg.waitingUrl = Number(lastFocusItem.id)
    if ([bg.S_FAILED, bg.S_PAUSED, bg.S_WAIT_URL].includes(bg.downloads[lastFocusItem.id].state)) {
        down.state = bg.S_WAIT_URL
        update(Number(lastFocusItem.id))
    }
})

document.getElementById('copy-url').addEventListener('click', () => {
    event.preventDefault()
    if (lastFocusItem == undefined) {
        return
    }
    navigator.clipboard.writeText(bg.downloads[lastFocusItem.id]?.url)
})

// ==================== SETTINGS ===================

let settingsElements

document.getElementById('settings-butt').addEventListener('click', event => {
    let downs = document.getElementsByTagName('ui-downloads')[0].style
    let setts = document.getElementsByTagName('ui-settings')[0].style
    if (downs.display == '' || downs.display == 'block') {
        // list shown, show settings
        event.target.innerText = 'Back'
        downs.display = 'none'
        setts.display = 'block'
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
    } else {
        event.target.innerText = 'Settings'
        downs.display = 'block'
        setts.display = 'none'
    }
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
    chrome.storage.local.get('settings', res => {
        if (res.settings == undefined) {
            return
        }
        settingsElements.conns.value = res.settings.conns
        let cats = []
        for (let [name, exts] of Object.entries(res.settings.categories)) {
            cats.push(name + ': ' + exts.join(' '))
        }
        settingsElements.categories.value = cats.join('\n')
        settingsElements.notify.begin.checked = res.settings.notify.begin
        settingsElements.notify.end.checked = res.settings.notify.end
    })
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
        retrieveSettings()
        bg.settings = settings
    })
})

