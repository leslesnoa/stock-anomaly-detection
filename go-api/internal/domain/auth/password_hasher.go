package auth

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) error
}
