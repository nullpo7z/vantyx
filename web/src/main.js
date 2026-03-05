import API from './api.js'
import { renderLogin } from './login.js'
import { renderApp } from './app.js'

const appEl = document.getElementById('app')

async function init() {
  try {
    await API.me()
    renderApp(appEl)
  } catch {
    renderLogin(appEl)
  }
}

init()
