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
    chrome.downloads.download({ url: urlInput.value })
    resetUrl()
}

function resetUrl() {
    urlInput.parentElement.style.display = 'none'
    toolbar.style.display = 'flex'
}

document.getElementById('add-url').addEventListener('click', commitUrl)
document.getElementById('cancel-url').addEventListener('click', resetUrl)

// ================ LIST-ITEMS ======================

let lastFocusId  // id of the item last focused

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
    [bg.S_REBUILDING]: 'Rebuilding...',
    [bg.S_WAIT_URL]: 'Waiting for new URL...',
}

class DownloadItem extends HTMLElement {

    constructor(...args) {
        super(...args)

        this.addEventListener('focus', () => {
            lastFocusId = Number(this.id)
        })
    }

    async connectedCallback() {
        this.setAttribute('tabindex', 0)
        this.data = bg.downloads[this.id]

        this.icon = document.createElement('img')
        this.icon.src = this.data.icon
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
            msgElm.innerText = staticMsgs[this.data.state] || this.data.error
            this.appendChild(msgElm)
            lastPartI++
        }
        for (let name of partsNames.slice(lastPartI)) {
            this[name].remove()
        }
    }
}

customElements.define('download-item', DownloadItem)

list = document.getElementsByTagName('ui-list')[0]
for (let id of Object.keys(bg.downloads)
    .sort((a, b) => a[1].date < b[1].date ? 1 : -1)  // by date, most recent
    .sort((a, b) => a[1].state < b[1].state ? -1 : 1)) {  // by status, in progress at the top
    let item = document.createElement('download-item')
    item.id = id
    list.appendChild(item)
}

function add(id) {
    let item = document.createElement('download-item')
    item.id = id
    list.insertAdjacentElement('afterbegin', item)
}

function update(id) {
    document.getElementById(id).update()
}

// ===================== LIST ======================

function finishRemove(id) {
    let item = document.getElementById(id)
    if (item == null) return
    item.remove()
}

document.getElementById('remove').addEventListener('click', event => {
    event.preventDefault()
    console.log(lastFocusId)
    let item = document.getElementById(lastFocusId)
    if (item == null) return
    if (bg.removeItem(lastFocusId)) {
        lastFocusId = undefined
    }
})

function pauseResume(event) {
    event.preventDefault()
    if (document.getElementById(lastFocusId) == null) return
    let stateTo = event.target.id == 'pause' ? bg.S_PAUSED : bg.S_DOWNLOADING
    bg.changeState(lastFocusId, stateTo)
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
    let item = document.getElementById(lastFocusId)
    if (item == null) return
    if (event.target.id == 'open') {
        bg.openFile(lastFocusId)
    } else {
        bg.openDir(lastFocusId)
    }
}

document.getElementById('open').addEventListener('click', openPath)
document.getElementById('folder').addEventListener('click', openPath)

document.getElementById('change-url').addEventListener('click', event => {
    event.preventDefault()
    let item = document.getElementById(lastFocusId)
    if (item == null) return
    let down = bg.downloads[lastFocusId]
    if (bg.waitingUrl) {
        let lastWaiting = bg.downloads[bg.waitingUrl].state
        if (lastWaiting.error) {
            lastWaiting.state = bg.S_FAILED
        } else {
            lastWaiting.state = bg.S_PAUSED
        }
    }
    bg.waitingUrl = lastFocusId
    if ([bg.S_FAILED, bg.S_PAUSED, bg.S_WAIT_URL].includes(bg.downloads[lastFocusId].state)) {
        down.state = bg.S_WAIT_URL
        update(lastFocusId)
    }
})

document.getElementById('copy-url').addEventListener('click', () => {
    event.preventDefault()
    if (lastFocusId == undefined) {
        return
    }
    navigator.clipboard.writeText(bg.downloads[lastFocusId]?.url)
})

// ==================== SETTINGS ===================

document.getElementById('settings-butt').addEventListener('click', event => {
    let downs = document.getElementsByTagName('ui-downloads')[0].style
    let setts = document.getElementsByTagName('ui-settings')[0].style
    if (downs.display == '' || downs.display == 'block') {
        // list shown, show settings
        event.target.innerText = 'Back'
        downs.display = 'none'
        setts.display = 'block'
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
    let catsElm = document.getElementById('categories')
    let connsElm = document.getElementById('conns')
    chrome.storage.local.get('settings', res => {
        if (res.settings == undefined) {
            return
        }
        connsElm.value = res.settings.conns
        let cats = []
        for (let [name, exts] of Object.entries(res.settings.categories)) {
            cats.push(name + ': ' + exts.join(' '))
        }
        catsElm.value = cats.join('\n')
    })
}

document.getElementById('save-settings').addEventListener('click', event => {
    event.preventDefault()
    let catsElm = document.getElementById('categories')
    let connsElm = document.getElementById('conns')
    let settings = {
        conns: Number(connsElm.value),
        categories: parseCats(catsElm.value)
    }
    chrome.storage.local.set({ settings }, () => {
        retrieveSettings()
        bg.settings = settings
    })
})
