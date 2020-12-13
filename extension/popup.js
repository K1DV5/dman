let tabs = ['progress', 'completed', 'options']
let currentTab = 'progress'

function selectTab(id) {
    for (let tab of tabs) {
        let element = document.getElementById(tab)
        let tabElem = document.querySelector('span[data-tab="' + tab + '"]')
        if (id == tab) {
            element.style.display = 'block'
            tabElem.classList.add('tab-current')
        } else {
            element.style.display = 'none'
            tabElem.classList.remove('tab-current')
        }
    }
}

selectTab(currentTab)

for (let element of document.getElementsByClassName('tab')) {
    element.addEventListener('click', () => {
        selectTab(element.dataset.tab)
    })
}

document.getElementById('add').addEventListener('click', () => {
    event.target.parentElement.style.display = 'none'
    document.querySelector('.input-url').style.display = 'flex'
    let urlInput = document.getElementById('url')
    urlInput.value = ''
    urlInput.focus()
})

document.getElementById('add-url').addEventListener('click', () => {
    event.target.parentElement.style.display = 'none'
    document.querySelector('.toolbar-prog').style.display = 'flex'
})

document.getElementById('cancel-url').addEventListener('click', () => {
    event.target.parentElement.style.display = 'none'
    document.querySelector('.toolbar-prog').style.display = 'flex'
})
