let bgPage = chrome.extension.getBackgroundPage()
let listShown = true

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
    row.id = 'item-' + id
    if (data.completed) {
        fnamePart.innerText = data.filename
    } else {
        let fname = document.createElement('div')
        fnamePart.appendChild(fname)
        fname.innerText = data.filename
        let progress = document.createElement('div')
        fnamePart.appendChild(progress)
        progress.className = 'progress'
        progress.style.width = data.percent
        let info = document.createElement('div')
        fnamePart.appendChild(info)
        info.className = 'info'
        let percent = document.createElement('span')
        info.appendChild(percent)
        percent.innerText = data.percent
        let speed = document.createElement('span')
        info.appendChild(speed)
        speed.innerText = data.speed
        let conns = document.createElement('span')
        info.appendChild(conns)
        conns.innerText = data.conns
        let eta = document.createElement('span')
        info.appendChild(eta)
        eta.innerText = data.eta
    }

    let sizePart = document.createElement('td')
    row.appendChild(sizePart)
    sizePart.innerText = data.size

    let datePart = document.createElement('td')
    row.appendChild(datePart)
    datePart.innerText = data.date
}

let inProgress = []
for (let [id, info] of Object.entries(bgPage.uiInfos)) {
    if (info.completed) {
        addRow(info, id)
    } else {
        inProgress.push([id, info])
    }
}
for (let [id, info] of inProgress) {
    addRow(info, id)
}

function commitUrl() {
    // send to native, then
    // let url = urlInput.value
    let id = Number((Date.now()).toString().slice(2, -2))
    let info = {
        filename: 'foo',
        size: '23.1MB',
        speed: '8MB/s',
        percent: '35%',
        conns: 'x13',
        eta: '5m23s',
        date: '12/16/2020'
    }
    bgPage.uiInfos[id] = info
    addRow(info, id)
    resetUrl()
}

function resetUrl() {
    urlInput.parentElement.style.display = 'none'
    toolbar.style.display = 'flex'
}

document.getElementById('add-url').addEventListener('click', commitUrl)
document.getElementById('cancel-url').addEventListener('click', resetUrl)

// ===================== LIST ======================


