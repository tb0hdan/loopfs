package loop

// ValidateHash checks if a hash string is valid format.
func (s *Store) ValidateHash(hash string) bool {
	if len(hash) != hashLength {
		return false
	}

	for _, char := range hash {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	return true
}
