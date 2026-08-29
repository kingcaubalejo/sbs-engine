package database

type Donation struct {
	Status int `json:"status"`
	Url string  `json:"url"`
}

func (d *Database) GetDonation() Donation {
	var donation Donation
	donation = Donation{
		Status: 200,
		Url: "https://www.sandbox.paypal.com/donate/?hosted_button_id=5RYBFV6ZQZ55A",
	}
	return donation
}