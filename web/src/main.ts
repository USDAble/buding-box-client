// OCTO-FORK: give the standalone loading screen a paint before evaluating the app graph.
requestAnimationFrame(() => {
  setTimeout(() => {
    void import('./bootstrap').catch(error => {
      // The index bootstrap owns startup errors, including deferred imports.
      window.dispatchEvent(new ErrorEvent('error', { error }))
      console.error('app bootstrap failed', error)
    })
  }, 0)
})
