let bg = chrome.extension.getBackgroundPage()

bg.switchUpdates(true)
window.addEventListener('close', () => bg.switchUpdates(false))

let lastFocusId

const urlInput = document.getElementById('url')
const list = document.getElementsByTagName('table')[0].tBodies[0]
const toolbar = document.getElementById('toolbar')


// ================ URL ======================
document.getElementById('add').addEventListener('click', () => {
    toolbar.style.display = 'none'
    urlInput.parentElement.style.display = 'flex'
    urlInput.value = ''
    urlInput.focus()
})

function addRow(data, id) {
    let row = list.insertRow(0)
    row.setAttribute('tabindex', 0)
    let iconPart = document.createElement('td')
    row.appendChild(iconPart)
    let icon = document.createElement('img')
    iconPart.appendChild(icon)
    icon.src = data.icon
    let fnamePart = document.createElement('td')
    row.appendChild(fnamePart)
    fnamePart.innerText = data.filename
    row.id = id
    if (data.state != bg.S_COMPLETED) {
        let progress = document.createElement('div')
        fnamePart.appendChild(progress)
        progress.className = 'progress'
        progress.appendChild(document.createElement('div'))
        for (let i = 0; i < 5; i++) {
            progress.appendChild(document.createElement('span'))
        }
        let [progressBar, percent, written, speed, eta, conns] = progress.children
        progressBar.style.width = data.percent + '%'
        if (data.state == bg.S_REBUILDING) {
            progressBar.style.background = 'lightgreen'
            percent.innerText = 'Rebuilding'
        } else if (data.state == bg.S_DOWNLOADING) {
            progressBar.style.background = 'cyan'
            percent.innerText = (Math.round(data.percent * 100) / 100) + '%'
            written.innerText = data.written
            speed.innerText = data.speed
            eta.innerText = data.eta
            conns.innerText = 'x' + data.conns
        } else if (data.state == bg.S_PAUSED) {
            progressBar.style.background = 'orange'
            percent.innerText = (Math.round(data.percent * 100) / 100) + '%'
            written.innerText = 'Paused'
        } else {  // failed
            progressBar.style.background = 'red'
            percent.innerText = (Math.round(data.percent * 100) / 100) + '%'
            written.innerText = data.error
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

function update(id) {
    let info = bg.downloads[id]
    let item = document.getElementById(id)
    let progress = item.children[1].firstElementChild
    if (info.state == bg.S_COMPLETED) {
        progress.remove()
    } else {
        let [progressBar, percent, written, speed, eta, conns] = progress.children
        if (info.state == bg.S_REBUILDING) {
            progressBar.style.background = 'lightgreen'
            progressBar.style.width = (Math.round(info.percent * 100) / 100) + '%'
            percent.innerHTML = 'Rebuilding'
            [written.innerText, speed.innerText, eta.innerText, conns.innerText] = ['', '', '', '']
        } else if (info.state == bg.S_FAILED) {
            progressBar.style.background = 'red'
            percent.innerText = (Math.round(info.percent * 100) / 100) + '%'
            written.innerText = info.error
            [speed.innerText, eta.innerText, conns.innerText] = ['', '', '']
        } else if (info.state == bg.S_PAUSED) {
            progressBar.style.background = 'orange'
            percent.innerText = (Math.round(info.percent * 100) / 100) + '%'
            written.innerText = info.written
            speed.innerText = 'Paused'
            [eta.innerText, conns.innerText] = ['', '']
        } else if (info.state == bg.S_DOWNLOADING) {
            progressBar.style.background = 'cyan'
            progressBar.style.width = info.percent + '%'
            percent.innerText = (Math.round(info.percent * 100) / 100) + '%'
            written.innerText = info.written
            speed.innerText = info.speed
            conns.innerText = 'x' + info.conns
            eta.innerText = info.eta
        }
    }
}

function commitUrl() {
    chrome.downloads.download({url: urlInput.value})
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
