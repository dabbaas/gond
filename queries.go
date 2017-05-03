package main

import (
    "log"
)

func verifyUUIDPubKey(uuid string, pubKey string) error {
    log.Printf(pubKey)
    return nil // TODO: Query dhakkan to check if user is authorized
    // return fmt.Errorf("pubkey rejected for %q", c.User())
}
//func fetchPodParams(uuid string) (string, error){
    // TODO: Query dhakkan to getch pod params
  //  return 
//}
