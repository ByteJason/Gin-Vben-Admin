package authplatform

import "golang.org/x/crypto/bcrypt"

type BcryptHasher struct{ Cost int }

func (h BcryptHasher) Hash(password string) (string, error) {
	cost := h.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(b), err
}

func (BcryptHasher) Compare(encoded, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password))
}
