let bg = chrome.extension.getBackgroundPage()

bg.switchUpdates(true)
window.addEventListener('close', () => bg.switchUpdates(false))

let lastFocusId

const urlInput = document.getElementById('url')
const list = document.getElementsByTagName('table')[0]
const toolbar = document.getElementById('toolbar')


// ================ URL ======================
document.getElementById('add').addEventListener('click', () => {
    toolbar.style.display = 'none'
    urlInput.parentElement.style.display = 'flex'
    urlInput.value = ''
    urlInput.focus()
})

function addRow(data, id) {
    let row = list.insertRow(1)
    row.setAttribute('tabindex', 0)

    let fnamePart = document.createElement('td')
    row.appendChild(fnamePart)
    row.id = id
    if (data.state == bg.S_COMPLETED) { // complete
        fnamePart.innerText = data.filename
    } else {
        let fname = document.createElement('div')
        fnamePart.appendChild(fname)
        fname.innerText = data.filename
        let progress = document.createElement('div')
        fnamePart.appendChild(progress)
        progress.className = 'progress'
        progress.style.width = data.percent + '%'
        let info = document.createElement('div')
        fnamePart.appendChild(info)
        info.className = 'info'
        if (data.state == bg.S_REBUILDING) {  // rebuilding
            progress.style.background = 'lightgreen'
            info.innerText = 'Rebuilding'
        } else {
            info.innerHTML = '<span></span>'.repeat(5)
            let [percent, written, speed, eta, conns] = info.children
            if (data.state == bg.S_DOWNLOADING) {  // downloading
                progress.style.background = 'cyan'
                percent.innerText = (Math.round(data.percent * 100) / 100) + '%'
                written.innerText = data.written
                speed.innerText = data.speed
                eta.innerText = data.eta
                conns.innerText = 'x' + data.conns
            } else if (data.state == bg.S_PAUSED) { // paused
                progress.style.background = 'orange'
                percent.innerText = (Math.round(data.percent * 100) / 100) + '%'
                written.innerText = 'Paused'
            } else {  // 2, failed
                progress.style.background = 'red'
                percent.innerText = (Math.round(data.percent * 100) / 100) + '%'
                written.innerText = data.error
            }
        }
    }

    let sizePart = document.createElement('td')
    row.appendChild(sizePart)
    sizePart.innerText = data.size

    let datePart = document.createElement('td')
    row.appendChild(datePart)
    datePart.innerText = data.date

    row.addEventListener('focus', () => lastFocusId = Number(id))
}

for (let [id, info] of Object.entries(bg.downloads).sort((a, b) => a[1].state < b[1].state ? 1 : -1)) {
    addRow(info, id)
}
if (list.rows.length == 1) {
    let caption = list.createCaption()
    caption.innerText = 'Downloads appear here.'
    caption.style.color = '#333'
}

function update(ids) {
    for (let id of ids) {
        let info = bg.downloads[id]
        let item = document.getElementById(id)
        if (info.state == bg.S_DOWNLOADING) {  // downloading
            let [_, progress, infoElm] = item.firstElementChild.children
            progress.style.background = 'cyan'
            progress.style.width = info.percent + '%'
            let [percent, written, speed, eta, conns] = infoElm.children
            percent.innerText = (Math.round(info.percent * 100) / 100) + '%'
            written.innerText = info.written
            speed.innerText = info.speed
            conns.innerText = 'x' + info.conns
            eta.innerText = info.eta
        } else if (info.state == bg.S_PAUSED) {  // paused
            let [_, progress, infoElm] = item.firstElementChild.children
            progress.style.background = 'orange'
            let [percent, written, speed, eta, conns] = infoElm.children
            percent.innerText = (Math.round(info.percent * 100) / 100) + '%'
            written.innerText = info.written
            speed.innerText = 'Paused'
            conns.innerText = ''
            eta.innerText = ''
        } else if (info.state == bg.S_FAILED) {  // failed
            let [_, progress, infoElm] = item.firstElementChild.children
            progress.style.background = 'red'
            let [percent, written, speed, eta, conns] = infoElm.children
            percent.innerText = (Math.round(info.percent * 100) / 100) + '%'
            written.innerText = info.error
            speed.innerText = ''
            conns.innerText = ''
            eta.innerText = ''
        } else if (info.state == bg.S_REBUILDING) {  // rebuilding
            let [_, progress, infoElm] = item.firstElementChild.children
            progress.style.background = 'lightgreen'
            progress.style.width = (Math.round(info.percent * 100) / 100) + '%'
            infoElm.innerHTML = 'Rebuilding'
        } else {  // 4, completed
            item.firstElementChild.innerHTML = info.filename
        }
    }
}

function commitUrl() {
    // list.deleteCaption()
    bg.addItem(urlInput.value)
    resetUrl()
}

function resetUrl() {
    urlInput.parentElement.style.display = 'none'
    toolbar.style.display = 'flex'
}

document.getElementById('add-url').addEventListener('click', commitUrl)
document.getElementById('cancel-url').addEventListener('click', resetUrl)

// ===================== LIST ======================

function finishRemove(id) {
    let item = document.getElementById(id)
    if (item == null) return
    item.remove()
    console.log(id)
}

document.getElementById('remove').addEventListener('click', event => {
    event.preventDefault()
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

// ==================== SETTINGS ===================

document.getElementById('settings-butt').addEventListener('click', event => {
    let downs = document.getElementById('downloads').style
    let setts = document.getElementById('settings').style
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
    chrome.storage.local.set({settings}, () => {
        retrieveSettings()
        bg.settings = settings
    })
})
