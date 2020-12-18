let bg = chrome.extension.getBackgroundPage()
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
            progress.style.background = 'cyan'
            info.innerText = 'Rebuilding'
        } else {
            let percent = document.createElement('span')
            info.appendChild(percent)
            let speed = document.createElement('span')
            info.appendChild(speed)
            let conns = document.createElement('span')
            info.appendChild(conns)
            let eta = document.createElement('span')
            info.appendChild(eta)
            if (data.state == bg.S_DOWNLOADING) {  // downloading
                progress.style.background = 'cyan'
                percent.innerText = data.percent + '%'
                speed.innerText = data.speed
                conns.innerText = 'x' + data.connections
                eta.innerText = data.eta
            } else if (data.state == bg.S_PAUSED) { // paused
                progress.style.background = 'orange'
                percent.innerText = data.percent + '%'
                speed.innerText = 'Paused'
            } else {  // 2, failed
                progress.style.background = 'red'
                percent.innerText = data.percent + '%'
                speed.innerText = 'Failed'
            }
        }
    }

    let sizePart = document.createElement('td')
    row.appendChild(sizePart)
    sizePart.innerText = data.size

    let datePart = document.createElement('td')
    row.appendChild(datePart)
    datePart.innerText = data.date

    row.addEventListener('focus', () => lastFocusId = id)
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
            progress.style.width = info.percent + '%'
            let [percent, speed, conns, eta] = infoElm.children
            percent.innerText = info.percent + '%'
            speed.innerText = info.speed
            conns.innerText = 'x' + info.connections
            eta.innerText = info.eta
        } else if (info.state == bg.S_PAUSED) {  // paused
            let [_, progress, infoElm] = item.firstElementChild.children
            progress.style.background = 'yellow'
            let [percent, speed, conns, eta] = infoElm.children
            percent.innerText = info.percent + '%'
            speed.innerText = 'Paused'
            conns.innerText = ''
            eta.innerText = ''
        } else if (info.state == bg.S_FAILED) {  // failed
            let [_, progress, infoElm] = item.firstElementChild.children
            progress.style.background = 'red'
            let [percent, speed, conns, eta] = infoElm.children
            percent.innerText = info.percent + '%'
            speed.innerText = 'Failed'
            conns.innerText = info.error
            eta.innerText = ''
        } else if (info.state == bg.S_REBUILDING) {  // rebuilding
            let [_, progress, infoElm] = item.firstElementChild.children
            progress.style.width = info.percent + '%'
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

function remove(event) {
    event.preventDefault()
    let item = document.getElementById(lastFocusId)
    if (item == null) return
    delete bg.downloads[lastFocusId]
    item.remove()
    lastFocusId = undefined
}

document.getElementById('remove').addEventListener('click', remove)
