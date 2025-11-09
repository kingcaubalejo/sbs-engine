package database

import "testing"

func TestGetDonation(t *testing.T) {
	db := &Database{}
	
	donation := db.GetDonation()
	
	if donation.Status != 200 {
		t.Errorf("Expected status 200, got %d", donation.Status)
	}
	
	expectedURL := "https://www.sandbox.paypal.com/donate/?hosted_button_id=5RYBFV6ZQZ55A"
	if donation.Url != expectedURL {
		t.Errorf("Expected URL %s, got %s", expectedURL, donation.Url)
	}
}