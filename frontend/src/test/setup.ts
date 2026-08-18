import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'

// vitest.config.ts n'active pas test.globals — RTL ne détecte donc pas
// automatiquement un afterEach global pour son nettoyage du DOM entre
// tests. Sans ce nettoyage explicite, le rendu d'un test précédent reste
// dans document.body et fait planter les requêtes getByRole/getByText du
// test suivant (plusieurs éléments correspondants trouvés).
afterEach(() => {
  cleanup()
})
